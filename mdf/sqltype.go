package mdf

import "time"

// SqlTypeKind is the discriminant for the SqlValue tagged union.
type SqlTypeKind int

const (
	SqlTinyInt          SqlTypeKind = iota
	SqlSmallInt         SqlTypeKind = iota
	SqlInt              SqlTypeKind = iota
	SqlBigInt           SqlTypeKind = iota
	SqlBinary           SqlTypeKind = iota
	SqlChar             SqlTypeKind = iota
	SqlNChar            SqlTypeKind = iota
	SqlVarBinary        SqlTypeKind = iota
	SqlVarChar          SqlTypeKind = iota
	SqlBit              SqlTypeKind = iota
	SqlVariant          SqlTypeKind = iota
	SqlNVarChar         SqlTypeKind = iota
	SqlSysName          SqlTypeKind = iota
	SqlDateTime         SqlTypeKind = iota
	SqlSmallDateTime    SqlTypeKind = iota
	SqlUniqueIdentifier SqlTypeKind = iota
	SqlImage            SqlTypeKind = iota
	SqlNText            SqlTypeKind = iota
	SqlFloat            SqlTypeKind = iota
)

// SqlType describes the SQL Server data type of a column, including size for
// fixed- and variable-length types.
type SqlType struct {
	Kind SqlTypeKind
	Size int // meaningful for Binary, Char, NChar, VarBinary, VarChar
}

// IsVarLength reports whether this type stores data in the variable-length
// offset array of a record (as opposed to the fixed-data region).
func (t SqlType) IsVarLength() bool {
	switch t.Kind {
	case SqlVarBinary, SqlVarChar, SqlSysName, SqlNVarChar, SqlVariant, SqlImage, SqlNText:
		return true
	}
	return false
}

// SqlTypeFromName maps a SQL Server type name (lowercase) to a SqlType.
// col.Length is passed in for sized types.
func SqlTypeFromName(name string, length int) SqlType {
	switch name {
	case "tinyint":
		return SqlType{Kind: SqlTinyInt}
	case "smallint":
		return SqlType{Kind: SqlSmallInt}
	case "int":
		return SqlType{Kind: SqlInt}
	case "bigint":
		return SqlType{Kind: SqlBigInt}
	case "binary", "timestamp", "rowversion":
		return SqlType{Kind: SqlBinary, Size: length}
	case "char":
		return SqlType{Kind: SqlChar, Size: length}
	case "nchar":
		return SqlType{Kind: SqlNChar, Size: length}
	case "varbinary":
		return SqlType{Kind: SqlVarBinary, Size: length}
	case "varchar", "text":
		return SqlType{Kind: SqlVarChar, Size: length}
	case "bit":
		return SqlType{Kind: SqlBit}
	case "nvarchar", "xml":
		return SqlType{Kind: SqlNVarChar}
	case "sysname":
		return SqlType{Kind: SqlSysName}
	case "uniqueidentifier":
		return SqlType{Kind: SqlUniqueIdentifier}
	case "datetime", "date", "time", "datetime2", "datetimeoffset":
		return SqlType{Kind: SqlDateTime}
	case "smalldatetime":
		return SqlType{Kind: SqlSmallDateTime}
	case "sql_variant":
		return SqlType{Kind: SqlVariant}
	case "image":
		return SqlType{Kind: SqlImage}
	case "ntext":
		return SqlType{Kind: SqlNText}
	case "float", "real":
		return SqlType{Kind: SqlFloat}
	case "money":
		return SqlType{Kind: SqlBigInt} // stored as int64 cents
	case "smallmoney":
		return SqlType{Kind: SqlInt}
	case "numeric", "decimal":
		return SqlType{Kind: SqlVarBinary, Size: length} // fallback
	default:
		return SqlType{Kind: SqlVarBinary, Size: length} // unknown → blob
	}
}

// SqlValue is a tagged union holding a parsed SQL Server value.
// nil *SqlValue means SQL NULL.
type SqlValue struct {
	Kind    SqlTypeKind
	IsLob   bool
	LobPtr  *LobPointer
	IntVal  int64
	FloatVal float64
	BoolVal bool
	TimeVal time.Time
	StrVal  string
	BlobVal []byte
}

func sqlInt(kind SqlTypeKind, v int64) *SqlValue {
	return &SqlValue{Kind: kind, IntVal: v}
}
func sqlFloat(v float64) *SqlValue {
	return &SqlValue{Kind: SqlFloat, FloatVal: v}
}
func sqlBool(v bool) *SqlValue {
	return &SqlValue{Kind: SqlBit, BoolVal: v}
}
func sqlTime(v time.Time) *SqlValue {
	return &SqlValue{Kind: SqlDateTime, TimeVal: v}
}
func sqlStr(kind SqlTypeKind, v string) *SqlValue {
	return &SqlValue{Kind: kind, StrVal: v}
}
func sqlBlob(kind SqlTypeKind, v []byte) *SqlValue {
	return &SqlValue{Kind: kind, BlobVal: v}
}
func sqlLob(kind SqlTypeKind, ptr *LobPointer) *SqlValue {
	return &SqlValue{Kind: kind, IsLob: true, LobPtr: ptr}
}
