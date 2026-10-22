package mdf

import "encoding/binary"

// RecordTagA encodes flags from the high nibble of the first byte of a record.
type RecordTagA byte

const (
	RecordTagAHasNullBitmap       RecordTagA = 1 << 0
	RecordTagAHasVarLengthColumns RecordTagA = 1 << 1
	RecordTagAHasVersioningTag    RecordTagA = 1 << 2
	RecordTagAHasValidTagB        RecordTagA = 1 << 3
)

// BitParser reads individual bits from a byte stream, packing 8 bits per byte.
// Bit columns in SQL Server records share bytes sequentially.
type BitParser struct {
	currentByte byte
	readBits    int // starts at 8 so first use fetches a new byte
}

// NewBitParser creates a BitParser ready to read bits.
func NewBitParser() *BitParser {
	return &BitParser{readBits: 8}
}

// ReadBit reads the next bit from data, advancing cursor when a new byte is needed.
func (b *BitParser) ReadBit(data []byte, cursor *int) bool {
	if b.readBits == 8 {
		if *cursor >= len(data) {
			return false
		}
		b.currentByte = data[*cursor]
		*cursor++
		b.readBits = 0
	}
	ret := (b.currentByte & 1) == 1
	b.currentByte >>= 1
	b.readBits++
	return ret
}

// VarLengthColumns provides access to variable-length column data within a record.
// The offset array entries are absolute positions within the full record; baseOffset
// is subtracted to get relative positions within the data slice.
type VarLengthColumns struct {
	data       []byte // from varDataStart to end of record
	count      uint16
	baseOffset int // = varDataStart (absolute position in full record bytes)
}

// Get returns (isComplex, dataSlice) for the idx-th variable-length column.
// Complex means the data is a LOB pointer rather than inline data.
func (v *VarLengthColumns) Get(idx uint16) (complex bool, data []byte) {
	if idx >= v.count {
		return false, nil
	}

	var start int
	if idx == 0 {
		start = 2 * int(v.count) // skip past the entire offset array
	} else {
		prevVal := binary.LittleEndian.Uint16(v.data[2*(int(idx)-1):])
		start = int(prevVal&0x7FFF) - v.baseOffset
	}

	endVal := binary.LittleEndian.Uint16(v.data[2*int(idx):])
	complex = (endVal & 0x8000) != 0
	end := int(endVal&0x7FFF) - v.baseOffset

	if start < 0 || end < start || end > len(v.data) {
		return complex, nil
	}
	return complex, v.data[start:end]
}

// Record represents a parsed SQL Server heap/index record.
type Record struct {
	TagA        RecordTagA
	ColumnCount uint16
	FixedData   []byte
	nullBitmap  []byte
	VarColumns  *VarLengthColumns
}

// IsColumnNull reports whether the column at nullBitmapIndex is null.
// Bit = 1 means NULL in SQL Server's null bitmap.
func (r *Record) IsColumnNull(idx int) bool {
	if len(r.nullBitmap) == 0 {
		return false
	}
	if idx >= len(r.nullBitmap)*8 {
		return false
	}
	byteIdx := idx / 8
	bitIdx := idx % 8
	return (r.nullBitmap[byteIdx] & (1 << bitIdx)) != 0
}

// ParseRecord parses a record from raw page data starting at offset 0.
// isIndex and pMinLen are taken from the page header for index page records.
func ParseRecord(data []byte, isIndex bool, pMinLen uint16) *Record {
	if len(data) < 4 {
		return nil
	}

	tagA := RecordTagA(data[0] >> 4)
	recordType := (data[0] & 0x0F) >> 1

	// Only Primary(0), Index(3), and Blob(4) record types are supported
	if recordType != 0 && recordType != 3 && recordType != 4 {
		return nil
	}

	var fixedDataLength, offset int
	if isIndex {
		fixedDataLength = int(pMinLen) - 1
		offset = int(pMinLen)
	} else {
		offs := binary.LittleEndian.Uint16(data[2:4])
		if offs < 4 {
			return nil
		}
		fixedDataLength = int(offs) - 4
		offset = int(offs)
	}

	if offset > len(data) {
		return nil
	}
	if offset+2 > len(data) {
		return nil
	}

	columnCount := binary.LittleEndian.Uint16(data[offset:])
	offset += 2

	var nullBitmap []byte
	if tagA&RecordTagAHasNullBitmap != 0 {
		nullBytes := (int(columnCount) + 7) / 8
		if offset+nullBytes <= len(data) {
			nullBitmap = data[offset : offset+nullBytes]
			offset += nullBytes
		}
	}

	var varColumns *VarLengthColumns
	if tagA&RecordTagAHasVarLengthColumns != 0 && offset+2 <= len(data) {
		varCount := binary.LittleEndian.Uint16(data[offset:])
		varDataStart := offset + 2
		varColumns = &VarLengthColumns{
			data:       data[varDataStart:],
			count:      varCount,
			baseOffset: varDataStart,
		}
	}

	var fixedData []byte
	end := 4 + fixedDataLength
	if end <= len(data) {
		fixedData = data[4:end]
	}

	return &Record{
		TagA:        tagA,
		ColumnCount: columnCount,
		FixedData:   fixedData,
		nullBitmap:  nullBitmap,
		VarColumns:  varColumns,
	}
}
