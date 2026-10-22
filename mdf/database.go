package mdf

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// BootPage holds the parsed data from MDF boot page (file 1, page 9).
type BootPage struct {
	DatabaseName   string
	FirstSysIndices PagePointer
}

// parseBootPage reads the boot record from page 1:9.
func parseBootPage(page *RawPage) (*BootPage, error) {
	record := page.GetRecord(0)
	if record == nil {
		return nil, fmt.Errorf("boot page has no records")
	}
	fd := record.FixedData
	if len(fd) < 518 {
		return nil, fmt.Errorf("boot page fixed data too short: %d", len(fd))
	}

	// Database name: UTF-16LE at bytes 48–304
	dbName := readUTF16LE16(fd, 48, 304)

	// FirstSysIndices pointer: bytes 512–518
	pp := ParsePagePointer(fd[512:518])
	if pp == nil {
		return nil, fmt.Errorf("boot page missing FirstSysIndices pointer")
	}

	return &BootPage{DatabaseName: dbName, FirstSysIndices: *pp}, nil
}

// MdfDatabase is the top-level structure that holds the parsed catalog
// for a SQL Server database within a .BAK file.
type MdfDatabase struct {
	Provider    PageProvider
	Boot        *BootPage
	AllocUnits  []SysAllocUnit
	RowSets     []SysRowSet
	SchObjs     []SysSchObj
	ColPars     []SysColPar
	ScalarTypes []SysScalarType
}

// OpenDatabase reads the boot page and system tables from the page provider.
func OpenDatabase(provider PageProvider) (*MdfDatabase, error) {
	db := &MdfDatabase{Provider: provider}

	// Boot page is always at file 1, page 9
	bootPage, err := provider.GetPage(PagePointer{PageID: 9, FileID: 1})
	if err != nil || bootPage == nil {
		return nil, fmt.Errorf("cannot read boot page (file 1, page 9): %w", err)
	}
	db.Boot, err = parseBootPage(bootPage)
	if err != nil {
		return nil, fmt.Errorf("cannot parse boot page: %w", err)
	}
	fmt.Printf("  Database: %s\n", db.Boot.DatabaseName)
	fmt.Println("  Reading system tables...")

	// Allocation units are on the FirstSysIndices page (traverse entire chain)
	allocPage, err := provider.GetPage(db.Boot.FirstSysIndices)
	if err != nil || allocPage == nil {
		return nil, fmt.Errorf("cannot read alloc units page %v: %w", db.Boot.FirstSysIndices, err)
	}
	for _, rec := range allocPage.AllRecords(false) {
		db.AllocUnits = append(db.AllocUnits, ParseSysAllocUnit(rec))
	}
	fmt.Printf("    %d allocation units\n", len(db.AllocUnits))

	// SysRowSet: find via well-known AuId
	rowSetAu := db.findAllocUnitByID(WellKnownIds.SysRowSetAuId, AllocUnitInRowData)
	if rowSetAu == nil || rowSetAu.PgFirst == nil {
		return nil, fmt.Errorf("cannot find SysRowSet allocation unit")
	}
	db.RowSets, err = db.readSystemTable(*rowSetAu.PgFirst, ParseSysRowSet)
	if err != nil {
		return nil, fmt.Errorf("cannot read SysRowSet: %w", err)
	}
	fmt.Printf("    %d row sets\n", len(db.RowSets))

	// SysSchObjs
	db.SchObjs, _ = db.readSystemTableByMajor(WellKnownIds.SysSchObjsIdMajor, ParseSysSchObj)
	fmt.Printf("    %d schema objects\n", len(db.SchObjs))

	// SysColPars
	db.ColPars, _ = db.readSystemTableByMajor(WellKnownIds.SysColParsIdMajor, ParseSysColPar)
	fmt.Printf("    %d column params\n", len(db.ColPars))

	// SysScalarTypes
	db.ScalarTypes, _ = db.readSystemTableByMajor(WellKnownIds.SysScalarTypesIdMajor, ParseSysScalarType)
	fmt.Printf("    %d scalar types\n", len(db.ScalarTypes))

	return db, nil
}

// UserTables returns all user table schema objects.
func (db *MdfDatabase) UserTables() []SysSchObj {
	var out []SysSchObj
	for _, o := range db.SchObjs {
		if o.Type == SchTypeUserTable {
			out = append(out, o)
		}
	}
	return out
}

// AllTables returns user and system tables.
func (db *MdfDatabase) AllTables() []SysSchObj {
	var out []SysSchObj
	for _, o := range db.SchObjs {
		if o.Type == SchTypeUserTable || o.Type == SchTypeSystemTable {
			out = append(out, o)
		}
	}
	return out
}

// ColumnsForTable returns column metadata for a schema object.
func (db *MdfDatabase) ColumnsForTable(obj SysSchObj) []SysColPar {
	var out []SysColPar
	for _, c := range db.ColPars {
		if c.Id == obj.Id {
			out = append(out, c)
		}
	}
	return out
}

// TypeForColumn resolves the scalar type for a column.
func (db *MdfDatabase) TypeForColumn(col SysColPar) *SysScalarType {
	for i, ty := range db.ScalarTypes {
		if ty.XType == col.XType && ty.Id <= 255 {
			return &db.ScalarTypes[i]
		}
	}
	return nil
}

// BuildTable constructs an MdfTable for the given schema object.
func (db *MdfDatabase) BuildTable(obj SysSchObj) (*MdfTable, error) {
	colPars := db.ColumnsForTable(obj)

	var columns []ColumnType
	for _, col := range colPars {
		scalarType := db.TypeForColumn(col)
		if scalarType == nil {
			continue
		}
		typeName := strings.ToLower(scalarType.Name)
		columns = append(columns, ColumnType{
			Idx:      col.ColId,
			Name:     col.Name,
			DataType: SqlTypeFromName(typeName, int(col.Length)),
			Nullable: col.Status&ColParNullable != 0,
			Computed: col.IsComputed(),
		})
	}

	schema := NewSchema(columns)

	// Find partition page pointers (idMinor <= 1 means data partition)
	var partitions []PagePointer
	for _, rs := range db.RowSets {
		if rs.IdMajor != obj.Id || rs.IdMinor > 1 {
			continue
		}
		au := db.findAllocUnitByOwner(rs.RowSetId, AllocUnitInRowData)
		if au != nil && au.PgFirst != nil {
			partitions = append(partitions, *au.PgFirst)
		}
	}

	return &MdfTable{
		Name:       obj.Name,
		Provider:   db.Provider,
		Schema:     schema,
		Partitions: partitions,
	}, nil
}

// ---- internal helpers ----

func (db *MdfDatabase) findAllocUnitByID(auID int64, typ AllocUnitType) *SysAllocUnit {
	for i, au := range db.AllocUnits {
		if au.AuId == auID && au.Type == typ {
			return &db.AllocUnits[i]
		}
	}
	return nil
}

func (db *MdfDatabase) findAllocUnitByOwner(ownerID int64, typ AllocUnitType) *SysAllocUnit {
	for i, au := range db.AllocUnits {
		if au.OwnerId == ownerID && au.Type == typ {
			return &db.AllocUnits[i]
		}
	}
	return nil
}

func (db *MdfDatabase) readSystemTable[T any](pgFirst PagePointer, parser func(*Record) T) ([]T, error) {
	page, err := db.Provider.GetPage(pgFirst)
	if err != nil || page == nil {
		return nil, err
	}
	var out []T
	page.ForEachRecord(false, func(rec *Record) bool {
		out = append(out, parser(rec))
		return true
	})
	return out, nil
}

func (db *MdfDatabase) readSystemTableByMajor[T any](idMajor int, parser func(*Record) T) ([]T, error) {
	rowSet := db.findRowSet(idMajor, 1)
	if rowSet == nil {
		return nil, nil
	}
	au := db.findAllocUnitByOwner(rowSet.RowSetId, AllocUnitInRowData)
	if au == nil || au.PgFirst == nil {
		return nil, nil
	}
	return db.readSystemTable(*au.PgFirst, parser)
}

func (db *MdfDatabase) findRowSet(idMajor, idMinor int) *SysRowSet {
	for i, rs := range db.RowSets {
		if rs.IdMajor == idMajor && rs.IdMinor == idMinor {
			return &db.RowSets[i]
		}
	}
	return nil
}

// ---- PagePointer debug helper ----

func (p PagePointer) String() string {
	return fmt.Sprintf("(%d:%d)", p.FileID, p.PageID)
}

// MdfTable represents a user table and its data pages.
type MdfTable struct {
	Name       string
	Provider   PageProvider
	Schema     *Schema
	Partitions []PagePointer // first page of each partition's linked list
}

// EachRow iterates all rows in the table, calling fn for each.
// LOB values are resolved inline using the page provider.
// fn should return false to stop early.
func (t *MdfTable) EachRow(fn func(row []*SqlValue) bool) {
	for _, partPtr := range t.Partitions {
		page, err := t.Provider.GetPage(partPtr)
		if err != nil || page == nil {
			continue
		}
		cont := true
		page.ForEachRecord(false, func(rec *Record) bool {
			row := t.Schema.ParseRow(rec)
			// Resolve any LOB pointers inline
			for i, v := range row {
				if v != nil && v.IsLob {
					row[i] = ResolveLob(v, t.Provider)
				}
			}
			cont = fn(row)
			return cont
		})
		if !cont {
			return
		}
	}
}

// ColumnNames returns column names in schema order (excluding computed columns).
func (t *MdfTable) ColumnNames() []string {
	var names []string
	for _, col := range t.Schema.Columns {
		if !col.Computed {
			names = append(names, col.Name)
		}
	}
	return names
}

// ActiveColumns returns ColumnType entries for non-computed columns.
func (t *MdfTable) ActiveColumns() []ColumnType {
	var out []ColumnType
	for _, col := range t.Schema.Columns {
		if !col.Computed {
			out = append(out, col)
		}
	}
	return out
}

// ReadBootDBName is a helper to read the database name from a binary page header offset.
// readUTF16LE16 reads a UTF-16LE string from data[start:end], stopping at null or 0x2020 padding.
func readUTF16LE16(data []byte, start, end int) string {
	if end > len(data) {
		end = len(data)
	}
	n := (end - start) / 2
	runes := make([]rune, 0, n)
	for i := start; i+1 < end; i += 2 {
		val := binary.LittleEndian.Uint16(data[i:])
		if val == 0 || val == 0x2020 {
			break
		}
		runes = append(runes, rune(val))
	}
	return strings.TrimSpace(string(runes))
}
