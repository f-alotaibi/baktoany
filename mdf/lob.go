package mdf

import "encoding/binary"

// LobType is the discriminant for a LOB entry on a TextMix/TextTree page.
type LobType uint16

const (
	LobTypeSmallRoot      LobType = 0
	LobTypeInternal       LobType = 2
	LobTypeData           LobType = 3
	LobTypeLargeRootYukon LobType = 5
	LobTypeNull           LobType = 8
)

// LobPointer is the 16-byte inline pointer stored in a var-length column
// when the actual data lives on a separate TextMix/TextTree page.
type LobPointer struct {
	Timestamp uint32
	Ptr       RecordPointer
}

// Read follows the LOB pointer chain and assembles the full byte slice.
// Returns nil if the LOB cannot be read.
func (lp *LobPointer) Read(provider PageProvider) []byte {
	rec, err := provider.GetRecord(lp.Ptr)
	if err != nil || rec == nil {
		return nil
	}
	entry := parseLobEntry(rec)
	if entry == nil {
		return nil
	}

	// BFS through the LOB tree to collect all data blocks
	var dataBlocks [][]byte
	queue := []*lobEntry{entry}

	for len(queue) > 0 {
		var next []*lobEntry
		for _, e := range queue {
			switch e.lobType {
			case LobTypeSmallRoot, LobTypeData:
				dataBlocks = append(dataBlocks, e.inlineData)
			default:
				for _, sub := range e.subEntries(provider) {
					if sub == nil {
						continue
					}
					if sub.lobType == LobTypeSmallRoot || sub.lobType == LobTypeData {
						dataBlocks = append(dataBlocks, sub.inlineData)
					} else {
						next = append(next, sub)
					}
				}
			}
		}
		queue = next
	}

	if len(dataBlocks) == 0 {
		return nil
	}
	total := 0
	for _, b := range dataBlocks {
		total += len(b)
	}
	result := make([]byte, 0, total)
	for _, b := range dataBlocks {
		result = append(result, b...)
	}
	return result
}

// lobEntry is the in-memory representation of a single LOB tree node.
type lobEntry struct {
	lobType    LobType
	inlineData []byte
	curLinks   uint16
	record     *Record // kept for sub-entry traversal
}

func parseLobEntry(record *Record) *lobEntry {
	if len(record.FixedData) < 10 {
		return nil
	}
	ty := LobType(binary.LittleEndian.Uint16(record.FixedData[8:10]))
	switch ty {
	case LobTypeSmallRoot:
		return parseLobSmallRoot(record)
	case LobTypeInternal:
		return parseLobInternal(record)
	case LobTypeData:
		return parseLobData(record)
	case LobTypeLargeRootYukon:
		return parseLobLargeRootYukon(record)
	default:
		return nil
	}
}

func parseLobSmallRoot(r *Record) *lobEntry {
	if len(r.FixedData) < 16 {
		return nil
	}
	length := binary.LittleEndian.Uint16(r.FixedData[10:12])
	end := 16 + int(length)
	if end > len(r.FixedData) {
		end = len(r.FixedData)
	}
	data := make([]byte, end-16)
	copy(data, r.FixedData[16:end])
	return &lobEntry{lobType: LobTypeSmallRoot, inlineData: data}
}

func parseLobData(r *Record) *lobEntry {
	data := make([]byte, len(r.FixedData)-10)
	copy(data, r.FixedData[10:])
	return &lobEntry{lobType: LobTypeData, inlineData: data}
}

func parseLobLargeRootYukon(r *Record) *lobEntry {
	if len(r.FixedData) < 16 {
		return nil
	}
	curLinks := binary.LittleEndian.Uint16(r.FixedData[12:14])
	return &lobEntry{lobType: LobTypeLargeRootYukon, curLinks: curLinks, record: r}
}

func parseLobInternal(r *Record) *lobEntry {
	if len(r.FixedData) < 16 {
		return nil
	}
	curLinks := binary.LittleEndian.Uint16(r.FixedData[12:14])
	return &lobEntry{lobType: LobTypeInternal, curLinks: curLinks, record: r}
}

// subEntries follows child pointers in a LargeRootYukon or Internal LOB node.
func (e *lobEntry) subEntries(provider PageProvider) []*lobEntry {
	out := make([]*lobEntry, e.curLinks)
	fixedData := e.record.FixedData

	for i := uint16(0); i < e.curLinks; i++ {
		var rp *RecordPointer

		switch e.lobType {
		case LobTypeLargeRootYukon:
			// SizedRecordPointer: 4-byte size + 8-byte RecordPointer, starting at offset 20
			off := 20 + 12*int(i)
			if off+12 > len(fixedData) {
				continue
			}
			rp = ParseRecordPointer(fixedData[off+4 : off+12])

		case LobTypeInternal:
			// RecordPointerWithOffset: 8-byte offset + 8-byte RecordPointer, at 16*(i+1)
			off := 16 * (int(i) + 1)
			if off+16 > len(fixedData) {
				continue
			}
			rp = ParseRecordPointer(fixedData[off+8 : off+16])
		}

		if rp == nil {
			continue
		}
		rec, err := provider.GetRecord(*rp)
		if err != nil || rec == nil {
			continue
		}
		out[i] = parseLobEntry(rec)
	}
	return out
}
