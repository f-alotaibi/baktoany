package mdf

import "sort"

// ColumnType describes a single column in a table schema.
type ColumnType struct {
	Idx      int    // column position (ColId for user tables, 0-based for system tables)
	Name     string
	DataType SqlType
	Nullable bool
	Computed bool
}

// Schema describes the full column layout of a table and handles record parsing.
type Schema struct {
	Columns []ColumnType // sorted by Idx ascending
}

// NewSchema creates a Schema, sorting columns by Idx.
func NewSchema(columns []ColumnType) *Schema {
	sorted := make([]ColumnType, len(columns))
	copy(sorted, columns)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Idx < sorted[j].Idx
	})
	return &Schema{Columns: sorted}
}

// ParseRow parses a Record into a slice of *SqlValue (nil = SQL NULL).
// The slice is indexed by column position in Schema.Columns.
func (s *Schema) ParseRow(record *Record) []*SqlValue {
	values := make([]*SqlValue, len(s.Columns))
	fixedCursor := 0
	bp := NewBitParser()
	var varIdx uint16
	nullBitIdx := 0

	for i, col := range s.Columns {
		if col.Computed {
			// Computed columns have no stored data; skip null bitmap too
			continue
		}

		if nullBitIdx >= int(record.ColumnCount) {
			// Past the record's declared column count — leave as nil (NULL)
		} else if !record.IsColumnNull(nullBitIdx) {
			if col.DataType.IsVarLength() {
				if record.VarColumns != nil {
					complex, data := record.VarColumns.Get(varIdx)
					if v, err := ParseVarLength(col.DataType, complex, data); err == nil {
						values[i] = v
					}
					varIdx++
				}
			} else {
				if v, err := ParseFixed(col.DataType, bp, record.FixedData, &fixedCursor); err == nil {
					values[i] = v
				} else {
					// On parse error advance cursor best-effort to stay aligned
					_ = err
				}
			}
		} else if col.DataType.IsVarLength() {
			// Null var-length columns do NOT have entries in the offset array;
			// varIdx is NOT incremented for null var-length columns.
		}

		nullBitIdx++
	}

	return values
}
