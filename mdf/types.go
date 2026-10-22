package mdf

import "encoding/binary"

// PagePointer identifies a specific 8KB page within the MDF.
type PagePointer struct {
	PageID uint32
	FileID uint16
}

// ParsePagePointer reads a 6-byte page pointer (4-byte pageID LE + 2-byte fileID LE).
// Returns nil if fileID is 0 (null pointer).
func ParsePagePointer(data []byte) *PagePointer {
	if len(data) < 6 {
		return nil
	}
	fileID := binary.LittleEndian.Uint16(data[4:6])
	if fileID == 0 {
		return nil
	}
	return &PagePointer{
		PageID: binary.LittleEndian.Uint32(data[0:4]),
		FileID: fileID,
	}
}

// RecordPointer identifies a specific record slot on a page.
type RecordPointer struct {
	PagePtr PagePointer
	SlotID  uint16
}

// ParseRecordPointer reads an 8-byte record pointer (6-byte page pointer + 2-byte slot ID).
func ParseRecordPointer(data []byte) *RecordPointer {
	if len(data) < 8 {
		return nil
	}
	pp := ParsePagePointer(data[0:6])
	if pp == nil {
		return nil
	}
	return &RecordPointer{
		PagePtr: *pp,
		SlotID:  binary.LittleEndian.Uint16(data[6:8]),
	}
}

// PageProvider provides random access to MDF pages by page pointer.
// Implemented by MtfPageProvider in the mtf package.
type PageProvider interface {
	GetPage(ptr PagePointer) (*RawPage, error)
	GetRecord(ptr RecordPointer) (*Record, error)
	FileIDs() []uint16
	NumPages(fileID uint16) uint32
}
