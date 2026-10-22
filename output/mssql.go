package output

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
	mssql "github.com/microsoft/go-mssqldb"

	"github.com/f-alotaibi/baktoany/mdf"
)

// ExportToMSSQL connects to SQL Server and restores all tables using
// the go-mssqldb bulk copy API (TDS bulk insert protocol).
func ExportToMSSQL(tables []*mdf.MdfTable, connStr string, opts WriteOpts) error {
	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return fmt.Errorf("mssql open: %w", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("mssql connect: %w", err)
	}
	fmt.Printf("Connected to SQL Server\n\n")

	for _, table := range tables {
		if err := mssqlWriteTable(db, table, opts); err != nil {
			fmt.Fprintf(os.Stderr, "  WARNING: %s: %v\n", table.Name, err)
		}
	}
	return nil
}

func mssqlWriteTable(db *sql.DB, table *mdf.MdfTable, opts WriteOpts) error {
	cols := table.ActiveColumns()
	if len(cols) == 0 {
		fmt.Printf("  %-40s  (no columns, skipped)\n", table.Name)
		return nil
	}

	if opts.DropFirst {
		if _, err := db.Exec(fmt.Sprintf("IF OBJECT_ID(N'%s', N'U') IS NOT NULL DROP TABLE [%s]",
			mssqlEscapeStr(table.Name), mssqlEscapeIdent(table.Name))); err != nil {
			return fmt.Errorf("drop table: %w", err)
		}
	}
	if opts.CreateTables {
		ddl := mssqlBuildDDL(table.Name, cols)
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}

	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = c.Name
	}

	// Use a dedicated transaction for bulk copy so any errors cleanly roll back
	// without leaving the connection in TDS bulk-insert mode.
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("tx begin: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// Use go-mssqldb bulk copy (TDS BCP protocol)
	escapedTable := "[" + mssqlEscapeIdent(table.Name) + "]"
	stmt, err := tx.Prepare(mssql.CopyIn(escapedTable, mssql.BulkOptions{}, colNames...))
	if err != nil {
		_ = tx.Rollback()
		// Fallback to batched INSERT if bulk copy isn't available
		return mssqlWriteBatched(db, table, cols)
	}

	var totalRows int64
	var writeErr error

	table.EachRow(func(row []*mdf.SqlValue) bool {
		args := make([]any, len(cols))
		for i := range cols {
			var v *mdf.SqlValue
			if i < len(row) {
				v = row[i]
			}
			args[i] = sqlValueToMSSQL(v, cols[i].DataType)
		}
		if _, err := stmt.Exec(args...); err != nil {
			writeErr = err
			return false
		}
		totalRows++
		return true
	})

	if writeErr != nil {
		_ = stmt.Close()
		return writeErr
	}

	// Finalise the bulk copy
	if _, err := stmt.Exec(); err != nil {
		_ = stmt.Close()
		return fmt.Errorf("bulk finalize: %w", err)
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("stmt close: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tx commit: %w", err)
	}

	fmt.Printf("  %-40s %8d rows\n", table.Name, totalRows)
	return nil
}

// mssqlWriteBatched is the fallback batched INSERT path.
func mssqlWriteBatched(db *sql.DB, table *mdf.MdfTable, cols []mdf.ColumnType) error {
	colQuoted := make([]string, len(cols))
	for i, c := range cols {
		colQuoted[i] = "[" + mssqlEscapeIdent(c.Name) + "]"
	}
	placeholders := "(" + strings.Repeat("?,", len(cols)-1) + "?)"
	baseInsert := fmt.Sprintf("INSERT INTO [%s] (%s) VALUES ",
		mssqlEscapeIdent(table.Name),
		strings.Join(colQuoted, ","))

	var batch [][]any
	var totalRows int64

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		rowPlaceholders := make([]string, len(batch))
		args := make([]any, 0, len(batch)*len(cols))
		for i, row := range batch {
			rowPlaceholders[i] = placeholders
			args = append(args, row...)
		}
		if _, err := db.Exec(baseInsert+strings.Join(rowPlaceholders, ","), args...); err != nil {
			return err
		}
		totalRows += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	var writeErr error
	table.EachRow(func(row []*mdf.SqlValue) bool {
		args := make([]any, len(cols))
		for i := range cols {
			var v *mdf.SqlValue
			if i < len(row) {
				v = row[i]
			}
			args[i] = sqlValueToMSSQL(v, cols[i].DataType)
		}
		batch = append(batch, args)
		if len(batch) >= mysqlBatchSize {
			if err := flush(); err != nil {
				writeErr = err
				return false
			}
		}
		return true
	})
	if writeErr != nil {
		return writeErr
	}
	if err := flush(); err != nil {
		return err
	}

	fmt.Printf("  %-40s %8d rows\n", table.Name, totalRows)
	return nil
}

// sqlValueToMSSQL converts an SqlValue to a Go value suitable for go-mssqldb.
func sqlValueToMSSQL(v *mdf.SqlValue, typ mdf.SqlType) any {
	if v == nil {
		return nil
	}
	switch typ.Kind {
	case mdf.SqlTinyInt, mdf.SqlSmallInt, mdf.SqlInt, mdf.SqlBigInt:
		// go-mssqldb bulk copy expects int64, int, or int32; passing int16 or uint8
		// triggers "mssql: invalid type for int column".
		return v.IntVal
	case mdf.SqlFloat:
		return v.FloatVal
	case mdf.SqlBit:
		return v.BoolVal
	case mdf.SqlDateTime, mdf.SqlSmallDateTime:
		if !v.TimeVal.IsZero() {
			return v.TimeVal
		}
		return nil
	case mdf.SqlUniqueIdentifier:
		if len(v.BlobVal) == 16 {
			return v.BlobVal
		}
		// Fallback: parse GUID string back to bytes if BlobVal was not populated
		var u mssql.UniqueIdentifier
		if err := u.Scan(v.StrVal); err == nil {
			return u[:]
		}
		return v.StrVal
	case mdf.SqlVarChar, mdf.SqlChar, mdf.SqlNVarChar, mdf.SqlNChar, mdf.SqlSysName:
		return strings.TrimRight(v.StrVal, "\x00")
	default:
		if len(v.StrVal) > 0 {
			return strings.TrimRight(v.StrVal, "\x00")
		}
		return v.BlobVal
	}
}

// mssqlBuildDDL generates CREATE TABLE IF NOT EXISTS for SQL Server.
func mssqlBuildDDL(tableName string, cols []mdf.ColumnType) string {
	var sb strings.Builder
	sb.WriteString("IF OBJECT_ID(N'[")
	sb.WriteString(mssqlEscapeIdent(tableName))
	sb.WriteString("]', N'U') IS NULL CREATE TABLE [")
	sb.WriteString(mssqlEscapeIdent(tableName))
	sb.WriteString("] (\n")
	for i, col := range cols {
		sb.WriteString("  [")
		sb.WriteString(mssqlEscapeIdent(col.Name))
		sb.WriteString("] ")
		sb.WriteString(mssqlSQLType(col.DataType))
		if col.Nullable {
			sb.WriteString(" NULL")
		} else {
			sb.WriteString(" NULL") // stay permissive when restoring
		}
		if i < len(cols)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(")")
	return sb.String()
}

// mssqlSQLType maps an MDF SqlType to a T-SQL type name.
func mssqlSQLType(t mdf.SqlType) string {
	switch t.Kind {
	case mdf.SqlTinyInt:
		return "TINYINT"
	case mdf.SqlSmallInt:
		return "SMALLINT"
	case mdf.SqlInt:
		return "INT"
	case mdf.SqlBigInt:
		return "BIGINT"
	case mdf.SqlFloat:
		return "FLOAT"
	case mdf.SqlBit:
		return "BIT"
	case mdf.SqlDateTime, mdf.SqlSmallDateTime:
		return "DATETIME2"
	case mdf.SqlUniqueIdentifier:
		return "UNIQUEIDENTIFIER"
	case mdf.SqlVarChar, mdf.SqlChar, mdf.SqlNVarChar, mdf.SqlNChar, mdf.SqlSysName:
		return "NVARCHAR(MAX)"
	default:
		return "VARBINARY(MAX)"
	}
}

// mssqlEscapeIdent escapes a SQL Server identifier (replaces ] with ]]).
func mssqlEscapeIdent(name string) string {
	return strings.ReplaceAll(name, "]", "]]")
}

// mssqlEscapeStr escapes a string literal (replaces ' with '').
func mssqlEscapeStr(name string) string {
	return strings.ReplaceAll(name, "'", "''")
}
