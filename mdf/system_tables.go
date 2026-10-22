package mdf

import "strings"

// WellKnownIds contains hardcoded allocation-unit / object IDs for SQL Server system tables.
var WellKnownIds = struct {
	SysRowSetAuId          int64
	SysSchObjsIdMajor      int
	SysColParsIdMajor      int
	SysScalarTypesIdMajor  int
}{
	SysRowSetAuId:         327680,
	SysSchObjsIdMajor:     34,
	SysColParsIdMajor:     41,
	SysScalarTypesIdMajor: 50,
}

// AllocUnitType is the type of a SysAllocUnit entry.
type AllocUnitType byte

const (
	AllocUnitDropped         AllocUnitType = 0
	AllocUnitInRowData       AllocUnitType = 1
	AllocUnitLobData         AllocUnitType = 2
	AllocUnitRowOverflowData AllocUnitType = 3
)

// SysAllocUnit is a row from the sysallocunits system table.
type SysAllocUnit struct {
	AuId       int64
	Type       AllocUnitType
	OwnerId    int64
	PgFirst    *PagePointer
	PgRoot     *PagePointer
	PgFirstIam *PagePointer
}

var sysAllocUnitSchema = NewSchema([]ColumnType{
	{Idx: 0, DataType: SqlType{Kind: SqlBigInt}, Name: "au_id"},
	{Idx: 1, DataType: SqlType{Kind: SqlTinyInt}, Name: "ty"},
	{Idx: 2, DataType: SqlType{Kind: SqlBigInt}, Name: "owner_id"},
	{Idx: 3, DataType: SqlType{Kind: SqlInt}, Name: "status"},
	{Idx: 4, DataType: SqlType{Kind: SqlSmallInt}, Name: "fgid"},
	{Idx: 5, DataType: SqlType{Kind: SqlBinary, Size: 6}, Name: "pg_first"},
	{Idx: 6, DataType: SqlType{Kind: SqlBinary, Size: 6}, Name: "pg_root"},
	{Idx: 7, DataType: SqlType{Kind: SqlBinary, Size: 6}, Name: "pg_firstiam"},
	{Idx: 8, DataType: SqlType{Kind: SqlBigInt}, Name: "pc_used"},
	{Idx: 9, DataType: SqlType{Kind: SqlBigInt}, Name: "pc_data"},
	{Idx: 10, DataType: SqlType{Kind: SqlBigInt}, Name: "pc_reserved"},
	{Idx: 11, DataType: SqlType{Kind: SqlInt}, Name: "db_frag_id", Nullable: true},
})

func ParseSysAllocUnit(record *Record) SysAllocUnit {
	row := sysAllocUnitSchema.ParseRow(record)
	au := SysAllocUnit{}
	if len(row) > 0 && row[0] != nil {
		au.AuId = row[0].IntVal
	}
	if len(row) > 1 && row[1] != nil {
		au.Type = AllocUnitType(byte(row[1].IntVal))
	}
	if len(row) > 2 && row[2] != nil {
		au.OwnerId = row[2].IntVal
	}
	if len(row) > 5 && row[5] != nil && len(row[5].BlobVal) >= 6 {
		au.PgFirst = ParsePagePointer(row[5].BlobVal)
	}
	if len(row) > 6 && row[6] != nil && len(row[6].BlobVal) >= 6 {
		au.PgRoot = ParsePagePointer(row[6].BlobVal)
	}
	if len(row) > 7 && row[7] != nil && len(row[7].BlobVal) >= 6 {
		au.PgFirstIam = ParsePagePointer(row[7].BlobVal)
	}
	return au
}

// SysRowSet is a row from sysrowsets.
type SysRowSet struct {
	RowSetId int64
	IdMajor  int
	IdMinor  int
}

var sysRowSetSchema = NewSchema([]ColumnType{
	{Idx: 0, DataType: SqlType{Kind: SqlBigInt}, Name: "row_set_id"},
	{Idx: 1, DataType: SqlType{Kind: SqlTinyInt}, Name: "owner_type"},
	{Idx: 2, DataType: SqlType{Kind: SqlInt}, Name: "id_major"},
	{Idx: 3, DataType: SqlType{Kind: SqlInt}, Name: "id_minor"},
	{Idx: 4, DataType: SqlType{Kind: SqlInt}, Name: "num_part"},
	{Idx: 5, DataType: SqlType{Kind: SqlInt}, Name: "status"},
	{Idx: 6, DataType: SqlType{Kind: SqlSmallInt}, Name: "fgidfs"},
	{Idx: 7, DataType: SqlType{Kind: SqlBigInt}, Name: "rcrows"},
	{Idx: 8, DataType: SqlType{Kind: SqlTinyInt}, Name: "cmpr_level", Nullable: true},
	{Idx: 9, DataType: SqlType{Kind: SqlTinyInt}, Name: "fill_fact", Nullable: true},
	{Idx: 10, DataType: SqlType{Kind: SqlInt}, Name: "max_leaf", Nullable: true},
	{Idx: 11, DataType: SqlType{Kind: SqlSmallInt}, Name: "max_int", Nullable: true},
	{Idx: 12, DataType: SqlType{Kind: SqlSmallInt}, Name: "min_leaf", Nullable: true},
	{Idx: 13, DataType: SqlType{Kind: SqlSmallInt}, Name: "min_int", Nullable: true},
	{Idx: 14, DataType: SqlType{Kind: SqlVarBinary}, Name: "rs_guid", Nullable: true},
	{Idx: 15, DataType: SqlType{Kind: SqlVarBinary}, Name: "lock_res", Nullable: true},
	{Idx: 16, DataType: SqlType{Kind: SqlInt}, Name: "db_frag_id", Nullable: true},
})

func ParseSysRowSet(record *Record) SysRowSet {
	row := sysRowSetSchema.ParseRow(record)
	rs := SysRowSet{}
	if len(row) > 0 && row[0] != nil {
		rs.RowSetId = row[0].IntVal
	}
	if len(row) > 2 && row[2] != nil {
		rs.IdMajor = int(row[2].IntVal)
	}
	if len(row) > 3 && row[3] != nil {
		rs.IdMinor = int(row[3].IntVal)
	}
	return rs
}

// SchType classifies a schema object.
type SchType int

const (
	SchTypeSystemTable SchType = iota
	SchTypeUserTable
	SchTypeOther
)

// SysSchObj is a row from sysschobjs (schema objects = tables, views, …).
type SysSchObj struct {
	Id   int
	Name string
	Type SchType
}

var sysSchObjSchema = NewSchema([]ColumnType{
	{Idx: 0, DataType: SqlType{Kind: SqlInt}, Name: "id"},
	{Idx: 1, DataType: SqlType{Kind: SqlSysName}, Name: "name"},
	{Idx: 2, DataType: SqlType{Kind: SqlInt}, Name: "ns_id"},
	{Idx: 3, DataType: SqlType{Kind: SqlTinyInt}, Name: "ns_class"},
	{Idx: 4, DataType: SqlType{Kind: SqlInt}, Name: "status"},
	{Idx: 5, DataType: SqlType{Kind: SqlChar, Size: 2}, Name: "ty"},
	{Idx: 6, DataType: SqlType{Kind: SqlInt}, Name: "pid"},
	{Idx: 7, DataType: SqlType{Kind: SqlTinyInt}, Name: "pcall"},
	{Idx: 8, DataType: SqlType{Kind: SqlInt}, Name: "int_prop"},
	{Idx: 9, DataType: SqlType{Kind: SqlDateTime}, Name: "created"},
	{Idx: 10, DataType: SqlType{Kind: SqlDateTime}, Name: "modified"},
})

func ParseSysSchObj(record *Record) SysSchObj {
	row := sysSchObjSchema.ParseRow(record)
	obj := SysSchObj{}
	if len(row) > 0 && row[0] != nil {
		obj.Id = int(row[0].IntVal)
	}
	if len(row) > 1 && row[1] != nil {
		obj.Name = strings.TrimRight(row[1].StrVal, "\x00")
	}
	if len(row) > 5 && row[5] != nil {
		tc := strings.TrimSpace(row[5].StrVal)
		switch tc {
		case "S":
			obj.Type = SchTypeSystemTable
		case "U":
			obj.Type = SchTypeUserTable
		default:
			obj.Type = SchTypeOther
		}
	}
	return obj
}

// ColParStatus are flags stored in the SysColPar status field.
type ColParStatus int

const (
	ColParNullable ColParStatus = 1 << 0
	ColParComputed ColParStatus = 1 << 4
)

// SysColPar is a row from syscolpars (column parameters).
type SysColPar struct {
	Id     int
	ColId  int
	Name   string
	XType  byte
	Length int
	Status ColParStatus
}

func (c SysColPar) IsComputed() bool {
	return c.Status&ColParComputed != 0
}

var sysColParSchema = NewSchema([]ColumnType{
	{Idx: 0, DataType: SqlType{Kind: SqlInt}, Name: "id"},
	{Idx: 1, DataType: SqlType{Kind: SqlSmallInt}, Name: "number"},
	{Idx: 2, DataType: SqlType{Kind: SqlInt}, Name: "col_id"},
	{Idx: 3, DataType: SqlType{Kind: SqlSysName}, Name: "name", Nullable: true},
	{Idx: 4, DataType: SqlType{Kind: SqlTinyInt}, Name: "xtype"},
	{Idx: 5, DataType: SqlType{Kind: SqlInt}, Name: "utype"},
	{Idx: 6, DataType: SqlType{Kind: SqlSmallInt}, Name: "length"},
	{Idx: 7, DataType: SqlType{Kind: SqlTinyInt}, Name: "prec"},
	{Idx: 8, DataType: SqlType{Kind: SqlTinyInt}, Name: "scale"},
	{Idx: 9, DataType: SqlType{Kind: SqlInt}, Name: "collation_id"},
	{Idx: 10, DataType: SqlType{Kind: SqlInt}, Name: "status"},
	{Idx: 11, DataType: SqlType{Kind: SqlSmallInt}, Name: "max_in_row"},
	{Idx: 12, DataType: SqlType{Kind: SqlInt}, Name: "xml_ns"},
	{Idx: 13, DataType: SqlType{Kind: SqlInt}, Name: "dflt"},
	{Idx: 14, DataType: SqlType{Kind: SqlInt}, Name: "chk"},
	{Idx: 15, DataType: SqlType{Kind: SqlVarBinary}, Name: "idt_val", Nullable: true},
})

func ParseSysColPar(record *Record) SysColPar {
	row := sysColParSchema.ParseRow(record)
	col := SysColPar{}
	if len(row) > 0 && row[0] != nil {
		col.Id = int(row[0].IntVal)
	}
	if len(row) > 2 && row[2] != nil {
		col.ColId = int(row[2].IntVal)
	}
	if len(row) > 3 && row[3] != nil {
		col.Name = strings.TrimRight(row[3].StrVal, "\x00")
	}
	if len(row) > 4 && row[4] != nil {
		col.XType = byte(row[4].IntVal)
	}
	if len(row) > 6 && row[6] != nil {
		col.Length = int(row[6].IntVal)
	}
	if len(row) > 10 && row[10] != nil {
		col.Status = ColParStatus(row[10].IntVal)
	}
	return col
}

// SysScalarType is a row from sysscalartypes (built-in and user-defined types).
type SysScalarType struct {
	Id    int
	Name  string
	XType byte
}

var sysScalarTypeSchema = NewSchema([]ColumnType{
	{Idx: 0, DataType: SqlType{Kind: SqlInt}, Name: "id"},
	{Idx: 1, DataType: SqlType{Kind: SqlInt}, Name: "sch_id"},
	{Idx: 2, DataType: SqlType{Kind: SqlSysName}, Name: "name"},
	{Idx: 3, DataType: SqlType{Kind: SqlTinyInt}, Name: "xtype"},
	{Idx: 4, DataType: SqlType{Kind: SqlSmallInt}, Name: "length"},
	{Idx: 5, DataType: SqlType{Kind: SqlTinyInt}, Name: "prec"},
	{Idx: 6, DataType: SqlType{Kind: SqlTinyInt}, Name: "scale"},
	{Idx: 7, DataType: SqlType{Kind: SqlInt}, Name: "collation_id"},
	{Idx: 8, DataType: SqlType{Kind: SqlInt}, Name: "status"},
	{Idx: 9, DataType: SqlType{Kind: SqlDateTime}, Name: "created"},
	{Idx: 10, DataType: SqlType{Kind: SqlDateTime}, Name: "modified"},
	{Idx: 11, DataType: SqlType{Kind: SqlInt}, Name: "dflt"},
	{Idx: 12, DataType: SqlType{Kind: SqlInt}, Name: "chk"},
})

func ParseSysScalarType(record *Record) SysScalarType {
	row := sysScalarTypeSchema.ParseRow(record)
	ty := SysScalarType{}
	if len(row) > 0 && row[0] != nil {
		ty.Id = int(row[0].IntVal)
	}
	if len(row) > 2 && row[2] != nil {
		ty.Name = strings.TrimRight(row[2].StrVal, "\x00")
	}
	if len(row) > 3 && row[3] != nil {
		ty.XType = byte(row[3].IntVal)
	}
	return ty
}
