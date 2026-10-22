package mdf

import "encoding/binary"

// PageType identifies the type of an 8KB MDF page.
type PageType byte

const (
	PageTypeUnAlloc    PageType = 0
	PageTypeData       PageType = 1
	PageTypeIndex      PageType = 2
	PageTypeTextMix    PageType = 3
	PageTypeTextTree   PageType = 4
	PageTypeSort       PageType = 7
	PageTypeGAM        PageType = 8
	PageTypeSGAM       PageType = 9
	PageTypeIAM        PageType = 10
	PageTypePFS        PageType = 11
	PageTypeBoot       PageType = 13
	PageTypeFileHeader PageType = 15
	PageTypeDiffMap    PageType = 16
	PageTypeMLMap      PageType = 17
)

// PageSize is the size of every MDF page in bytes.
const PageSize = 8192

// PageHeader represents the 96-byte header of an MDF page.
type PageHeader struct {
	Ptr       PagePointer
	Type      PageType
	Level     byte
	PMinLen   uint16
	SlotCount uint16
	ObjectID  uint32
	IndexID   uint16
	PrevPage  *PagePointer
	NextPage  *PagePointer
}

// ParsePageHeader parses the first 96 bytes of a page as a header.
func ParsePageHeader(data []byte) *PageHeader {
	if len(data) < 96 {
		return nil
	}
	h := &PageHeader{
		Type:      PageType(data[1]),
		Level:     data[3],
		IndexID:   binary.LittleEndian.Uint16(data[6:8]),
		PMinLen:   binary.LittleEndian.Uint16(data[14:16]),
		SlotCount: binary.LittleEndian.Uint16(data[22:24]),
		ObjectID:  binary.LittleEndian.Uint32(data[24:28]),
		PrevPage:  ParsePagePointer(data[8:14]),
		NextPage:  ParsePagePointer(data[16:22]),
	}
	// Self-pointer at offset 32
	if pp := ParsePagePointer(data[32:]); pp != nil {
		h.Ptr = *pp
	}
	return h
}

// RawPage represents a single 8KB MDF page.
type RawPage struct {
	Header   *PageHeader
	Data     []byte // full 8192 bytes
	Provider PageProvider
}

// NewRawPage creates a RawPage from raw 8192-byte data.
func NewRawPage(data []byte, provider PageProvider) *RawPage {
	if len(data) < PageSize {
		return nil
	}
	hdr := ParsePageHeader(data)
	if hdr == nil {
		return nil
	}
	return &RawPage{
		Header:   hdr,
		Data:     data[:PageSize],
		Provider: provider,
	}
}

// RecordCount returns the number of slots on this page.
func (p *RawPage) RecordCount() int {
	return int(p.Header.SlotCount)
}

// GetRecord returns the parsed Record at the given slot index, or nil.
func (p *RawPage) GetRecord(idx uint16) *Record {
	if int(idx) >= p.RecordCount() {
		return nil
	}
	// Slot array is at the tail of the page, growing backwards
	slotPos := PageSize - 2*int(idx) - 2
	if slotPos+2 > len(p.Data) {
		return nil
	}
	offset := int(binary.LittleEndian.Uint16(p.Data[slotPos:]))
	if offset >= len(p.Data) {
		return nil
	}
	return ParseRecord(p.Data[offset:], p.Header.Type == PageTypeIndex, p.Header.PMinLen)
}

// ForEachRecord iterates all records on this page and linked pages via NextPage.
// If localOnly is true, only records on this page are iterated.
// Stops early if fn returns false.
func (p *RawPage) ForEachRecord(localOnly bool, fn func(*Record) bool) {
	current := p
	for current != nil {
		for i := 0; i < current.RecordCount(); i++ {
			rec := current.GetRecord(uint16(i))
			if rec != nil {
				if !fn(rec) {
					return
				}
			}
		}
		if localOnly || current.Header.NextPage == nil || current.Provider == nil {
			break
		}
		next, err := current.Provider.GetPage(*current.Header.NextPage)
		if err != nil || next == nil {
			break
		}
		current = next
	}
}

// AllRecords collects all records (following NextPage links) into a slice.
// Used for small system tables where loading all records at once is fine.
func (p *RawPage) AllRecords(localOnly bool) []*Record {
	var out []*Record
	p.ForEachRecord(localOnly, func(r *Record) bool {
		out = append(out, r)
		return true
	})
	return out
}
