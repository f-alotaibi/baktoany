package mtf

import (
	"encoding/binary"
	"io"
	"os"
)

// FindMqdaStream scans an MTF-format .BAK file and returns the byte offset
// and length of the MQDA stream (which contains sequential 8KB MDF pages).
// The returned offset already skips the 2-byte MQDA header.
func FindMqdaStream(filePath string) (offset, length int64, found bool, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, 0, false, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return 0, 0, false, err
	}
	fileLen := fi.Size()

	headerBuf := make([]byte, 52)
	streamBuf := make([]byte, 22)

	for {
		dblkPos, _ := f.Seek(0, io.SeekCurrent)
		if dblkPos+52 > fileLen {
			break
		}

		n, err := io.ReadFull(f, headerBuf)
		if err != nil || n < 52 {
			break
		}

		// Accept ANY block type — SQL Server uses non-standard extensions (MSCI, MQDA).
		// Validate via XOR checksum only.
		if !verifyChecksum(headerBuf) {
			break
		}

		offsetToFirstStream := int64(binary.LittleEndian.Uint16(headerBuf[8:10]))
		streamPos := dblkPos + offsetToFirstStream
		if streamPos >= fileLen {
			break
		}
		if _, err := f.Seek(streamPos, io.SeekStart); err != nil {
			break
		}

		// Iterate streams attached to this DBLK
		done := false
		for !done {
			streamStart, _ := f.Seek(0, io.SeekCurrent)
			if streamStart+22 > fileLen {
				done = true
				break
			}

			n, err := io.ReadFull(f, streamBuf)
			if err != nil || n < 22 {
				done = true
				break
			}

			// If the first four bytes match a known DBLK type, we've overshot
			// into the next block — back up.
			if isKnownDblkType(binary.LittleEndian.Uint32(streamBuf[0:4])) {
				_, _ = f.Seek(streamStart, io.SeekStart)
				done = true
				break
			}

			if !verifyChecksum(streamBuf) {
				_, _ = f.Seek(streamStart, io.SeekStart)
				done = true
				break
			}

			streamID := string(streamBuf[0:4])
			dataLen := int64(binary.LittleEndian.Uint64(streamBuf[8:16]))
			dataStart, _ := f.Seek(0, io.SeekCurrent)

			if streamID == "MQDA" {
				// Skip the 2-byte undocumented MQDA header
				return dataStart + 2, dataLen - 2, true, nil
			}

			// Advance to next stream (4-byte aligned)
			nextPos := dataStart + dataLen
			if leftOver := nextPos % 4; leftOver > 0 {
				nextPos += 4 - leftOver
			}
			if nextPos <= streamStart {
				done = true
				break
			}
			_, _ = f.Seek(nextPos, io.SeekStart)

			if streamID == "SPAD" {
				done = true // SPAD marks end of streams for this DBLK
				break
			}
		}
	}

	return 0, 0, false, nil
}

// isKnownDblkType returns true if the 4-byte little-endian value matches
// a standard MTF DBLK type tag. Stream IDs like MQDA/SPAD are NOT in this list.
func isKnownDblkType(typeID uint32) bool {
	switch typeID {
	case 0x45504154, // TAPE
		0x54455353, // SSET
		0x424C4F56, // VOLB
		0x42524944, // DIRB
		0x454C4946, // FILE
		0x4C494643, // CFIL
		0x42505345, // ESPB
		0x54455345, // ESET
		0x4D544F45, // EOTM
		0x424D4653: // SFMB
		return true
	}
	return false
}

// verifyChecksum checks the XOR-only checksum used by MTF headers and stream headers.
// All 16-bit words must XOR to 0.
func verifyChecksum(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	var xor uint16
	for i := 0; i+1 < len(data); i += 2 {
		xor ^= binary.LittleEndian.Uint16(data[i : i+2])
	}
	if len(data)%2 != 0 {
		xor ^= uint16(data[len(data)-1])
	}
	return xor == 0
}
