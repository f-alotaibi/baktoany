package mtf

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/f-alotaibi/baktoany/mdf"
)

// MtfPageProvider implements mdf.PageProvider backed by a memory-mapped region
// of the .BAK file corresponding to the MQDA stream.
// Pages are read via os.File.ReadAt (OS page-cache backed, no full load into RAM).
type MtfPageProvider struct {
	file        *os.File
	mqdaOffset  int64
	mqdaLength  int64
	pageOffsets map[pageKey]int64
	fileIDs     []uint16
}

type pageKey struct {
	fileID uint16
	pageID uint32
}

// NewPageProvider opens the file, scans the MQDA stream to build the page index,
// and returns a ready PageProvider. Caller must call Close() when done.
func NewPageProvider(filePath string, mqdaOffset, mqdaLength int64) (*MtfPageProvider, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	pp := &MtfPageProvider{
		file:        f,
		mqdaOffset:  mqdaOffset,
		mqdaLength:  mqdaLength,
		pageOffsets: make(map[pageKey]int64),
	}
	pp.buildPageIndex()
	return pp, nil
}

// Close releases the underlying file handle.
func (pp *MtfPageProvider) Close() error {
	return pp.file.Close()
}

// buildPageIndex scans the MQDA stream sequentially, reading the 96-byte page
// header of each 8KB page to extract (fileID, pageID) → physical offset.
func (pp *MtfPageProvider) buildPageIndex() {
	const pageSize = int64(mdf.PageSize)
	buf := make([]byte, 96)
	var physOffset int64

	for physOffset+pageSize <= pp.mqdaLength {
		absOffset := pp.mqdaOffset + physOffset
		n, err := pp.file.ReadAt(buf, absOffset)
		if err != nil || n < 96 {
			break
		}

		pageID := binary.LittleEndian.Uint32(buf[32:36])
		fileID := binary.LittleEndian.Uint16(buf[36:38])

		if pageID > 0 && fileID > 0 {
			k := pageKey{fileID: fileID, pageID: pageID}
			if _, exists := pp.pageOffsets[k]; !exists {
				pp.pageOffsets[k] = physOffset
			}
		}

		physOffset += pageSize
	}

	// Collect unique file IDs
	seen := make(map[uint16]struct{})
	for k := range pp.pageOffsets {
		if _, ok := seen[k.fileID]; !ok {
			seen[k.fileID] = struct{}{}
			pp.fileIDs = append(pp.fileIDs, k.fileID)
		}
	}
}

// GetPage returns the RawPage for the given pointer, or nil if not found.
func (pp *MtfPageProvider) GetPage(ptr mdf.PagePointer) (*mdf.RawPage, error) {
	k := pageKey{fileID: ptr.FileID, pageID: ptr.PageID}
	physOffset, ok := pp.pageOffsets[k]
	if !ok {
		return nil, nil
	}
	absOffset := pp.mqdaOffset + physOffset
	data := make([]byte, mdf.PageSize)
	if _, err := pp.file.ReadAt(data, absOffset); err != nil {
		return nil, fmt.Errorf("read page (%d:%d): %w", ptr.FileID, ptr.PageID, err)
	}
	return mdf.NewRawPage(data, pp), nil
}

// GetRecord returns the Record at the given record pointer.
func (pp *MtfPageProvider) GetRecord(ptr mdf.RecordPointer) (*mdf.Record, error) {
	page, err := pp.GetPage(ptr.PagePtr)
	if err != nil || page == nil {
		return nil, err
	}
	return page.GetRecord(ptr.SlotID), nil
}

// FileIDs returns all file IDs seen in the page index.
func (pp *MtfPageProvider) FileIDs() []uint16 {
	return pp.fileIDs
}

// NumPages returns the highest page ID seen for a given file ID.
func (pp *MtfPageProvider) NumPages(fileID uint16) uint32 {
	var max uint32
	for k := range pp.pageOffsets {
		if k.fileID == fileID && k.pageID > max {
			max = k.pageID
		}
	}
	return max
}

// PageCount returns the total number of indexed pages.
func (pp *MtfPageProvider) PageCount() int {
	return len(pp.pageOffsets)
}
