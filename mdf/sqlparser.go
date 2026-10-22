package mdf

import (
	"encoding/binary"
	"fmt"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/text/encoding/charmap"
)

// ParseFixed parses a fixed-length column value from fixedData, advancing *cursor.
// bitParser is shared across all Bit columns in a row.
func ParseFixed(typ SqlType, bp *BitParser, fixedData []byte, cursor *int) (*SqlValue, error) {
	switch typ.Kind {
	case SqlTinyInt:
		if *cursor >= len(fixedData) {
			return nil, fmt.Errorf("out of bounds TinyInt")
		}
		v := int8(fixedData[*cursor])
		*cursor++
		return sqlInt(SqlTinyInt, int64(v)), nil

	case SqlSmallInt:
		if *cursor+2 > len(fixedData) {
			return nil, fmt.Errorf("out of bounds SmallInt")
		}
		v := int16(binary.LittleEndian.Uint16(fixedData[*cursor:]))
		*cursor += 2
		return sqlInt(SqlSmallInt, int64(v)), nil

	case SqlInt:
		if *cursor+4 > len(fixedData) {
			return nil, fmt.Errorf("out of bounds Int")
		}
		v := int32(binary.LittleEndian.Uint32(fixedData[*cursor:]))
		*cursor += 4
		return sqlInt(SqlInt, int64(v)), nil

	case SqlBigInt:
		if *cursor+8 > len(fixedData) {
			return nil, fmt.Errorf("out of bounds BigInt")
		}
		v := int64(binary.LittleEndian.Uint64(fixedData[*cursor:]))
		*cursor += 8
		return sqlInt(SqlBigInt, v), nil

	case SqlBit:
		return sqlBool(bp.ReadBit(fixedData, cursor)), nil

	case SqlFloat:
		if *cursor+8 > len(fixedData) {
			return nil, fmt.Errorf("out of bounds Float")
		}
		bits := binary.LittleEndian.Uint64(fixedData[*cursor:])
		*cursor += 8
		return sqlFloat(float64FromBits(bits)), nil

	case SqlUniqueIdentifier:
		if *cursor+16 > len(fixedData) {
			return nil, fmt.Errorf("out of bounds GUID")
		}
		b := fixedData[*cursor : *cursor+16]
		*cursor += 16
		return sqlStr(SqlUniqueIdentifier, formatGUID(b)), nil

	case SqlDateTime:
		// 4 bytes time (1/300s ticks) + 4 bytes date (days since 1900-01-01)
		if *cursor+8 > len(fixedData) {
			return nil, fmt.Errorf("out of bounds DateTime")
		}
		ticks := int32(binary.LittleEndian.Uint32(fixedData[*cursor:]))
		days := int32(binary.LittleEndian.Uint32(fixedData[*cursor+4:]))
		*cursor += 8
		base := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
		if days > 0 && days < 1_000_000 {
			base = base.AddDate(0, 0, int(days))
		}
		ms := int64(ticks) * 1000 / 300
		base = base.Add(time.Duration(ms) * time.Millisecond)
		return sqlTime(base), nil

	case SqlSmallDateTime:
		// 2 bytes time (minutes since midnight) + 2 bytes date (days since 1900-01-01)
		if *cursor+4 > len(fixedData) {
			return nil, fmt.Errorf("out of bounds SmallDateTime")
		}
		mins := binary.LittleEndian.Uint16(fixedData[*cursor:])
		days := binary.LittleEndian.Uint16(fixedData[*cursor+2:])
		*cursor += 4
		base := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).
			AddDate(0, 0, int(days)).
			Add(time.Duration(mins) * time.Minute)
		return sqlTime(base), nil

	case SqlBinary:
		if *cursor+typ.Size > len(fixedData) {
			return nil, fmt.Errorf("out of bounds Binary")
		}
		b := make([]byte, typ.Size)
		copy(b, fixedData[*cursor:*cursor+typ.Size])
		*cursor += typ.Size
		return sqlBlob(SqlBinary, b), nil

	case SqlChar:
		if *cursor+typ.Size > len(fixedData) {
			return nil, fmt.Errorf("out of bounds Char")
		}
		s := decodeVarChar(fixedData[*cursor : *cursor+typ.Size])
		*cursor += typ.Size
		return sqlStr(SqlChar, s), nil

	case SqlNChar:
		if *cursor+typ.Size > len(fixedData) {
			return nil, fmt.Errorf("out of bounds NChar")
		}
		s := decodeUTF16LE(fixedData[*cursor : *cursor+typ.Size])
		*cursor += typ.Size
		return sqlStr(SqlNChar, s), nil

	default:
		return nil, fmt.Errorf("unsupported fixed type: %d", typ.Kind)
	}
}

// ParseVarLength parses a variable-length column value from raw bytes.
// complex=true means the data holds a LOB pointer rather than inline content.
func ParseVarLength(typ SqlType, complex bool, data []byte) (*SqlValue, error) {
	switch typ.Kind {
	case SqlVarBinary:
		if complex {
			if len(data) < 16 {
				return nil, fmt.Errorf("LOB pointer too short")
			}
			ptr := parseLobPointer(data)
			if ptr == nil {
				return nil, fmt.Errorf("invalid LOB pointer")
			}
			return sqlLob(SqlVarBinary, ptr), nil
		}
		b := make([]byte, len(data))
		copy(b, data)
		return sqlBlob(SqlVarBinary, b), nil

	case SqlVarChar:
		if complex {
			if len(data) < 16 {
				return nil, fmt.Errorf("LOB pointer too short")
			}
			ptr := parseLobPointer(data)
			if ptr == nil {
				return nil, fmt.Errorf("invalid LOB pointer")
			}
			return sqlLob(SqlVarChar, ptr), nil
		}
		return sqlStr(SqlVarChar, decodeVarChar(data)), nil

	case SqlNVarChar:
		if complex {
			if len(data) < 16 {
				return nil, fmt.Errorf("LOB pointer too short")
			}
			ptr := parseLobPointer(data)
			if ptr == nil {
				return nil, fmt.Errorf("invalid LOB pointer")
			}
			return sqlLob(SqlNVarChar, ptr), nil
		}
		return sqlStr(SqlNVarChar, decodeUTF16LE(data)), nil

	case SqlSysName:
		return sqlStr(SqlSysName, decodeUTF16LE(data)), nil

	case SqlVariant:
		b := make([]byte, len(data))
		copy(b, data)
		return sqlBlob(SqlVariant, b), nil

	case SqlImage, SqlNText:
		if len(data) >= 16 && complex {
			ptr := parseLobPointer(data)
			if ptr != nil {
				return sqlLob(typ.Kind, ptr), nil
			}
		}
		if len(data) == 0 {
			return sqlBlob(typ.Kind, nil), nil
		}
		b := make([]byte, len(data))
		copy(b, data)
		return sqlBlob(typ.Kind, b), nil

	default:
		return nil, fmt.Errorf("unsupported var-length type: %d", typ.Kind)
	}
}

// ResolveLob resolves a LOB pointer to its actual bytes using the page provider.
// Returns nil if the LOB cannot be read.
func ResolveLob(v *SqlValue, provider PageProvider) *SqlValue {
	if v == nil || !v.IsLob || v.LobPtr == nil {
		return v
	}
	data := v.LobPtr.Read(provider)
	if data == nil {
		return nil
	}
	switch v.Kind {
	case SqlNVarChar, SqlNText:
		return sqlStr(v.Kind, decodeUTF16LE(data))
	default:
		return sqlBlob(v.Kind, data)
	}
}

// decodeVarChar decodes bytes as UTF-8, falling back to Windows-1252.
func decodeVarChar(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	decoded, err := charmap.Windows1252.NewDecoder().Bytes(data)
	if err != nil {
		return string(data) // last resort
	}
	return string(decoded)
}

// decodeUTF16LE decodes bytes as UTF-16 little-endian, stopping at the first null character.
func decodeUTF16LE(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	n := len(data) / 2
	runes := make([]rune, 0, n)
	for i := 0; i+1 < len(data); i += 2 {
		r := rune(binary.LittleEndian.Uint16(data[i:]))
		if r == 0 {
			break
		}
		runes = append(runes, r)
	}
	return string(runes)
}

// formatGUID formats 16 bytes as a mixed-endian UUID string.
// SQL Server GUIDs store Data1/2/3 in little-endian and Data4 in big-endian.
func formatGUID(b []byte) string {
	d1 := binary.LittleEndian.Uint32(b[0:4])
	d2 := binary.LittleEndian.Uint16(b[4:6])
	d3 := binary.LittleEndian.Uint16(b[6:8])
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		d1, d2, d3, b[8:10], b[10:16])
}

// float64FromBits reinterprets a uint64 as float64.
func float64FromBits(bits uint64) float64 {
	// math.Float64frombits
	return *(*float64)(unsafe.Pointer(&bits))
}

// parseLobPointer parses the 16-byte LOB pointer from var-length column data.
func parseLobPointer(data []byte) *LobPointer {
	if len(data) < 16 {
		return nil
	}
	timestamp := binary.LittleEndian.Uint32(data[0:4])
	rp := ParseRecordPointer(data[8:16])
	if rp == nil {
		return nil
	}
	return &LobPointer{Timestamp: timestamp, Ptr: *rp}
}
