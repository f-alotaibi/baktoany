package output

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/f-alotaibi/baktoany/mdf"
)

// ExportToMySQL connects to MySQL/MariaDB and restores all tables using
// batched multi-row INSERT statements.
func ExportToMySQL(tables []*mdf.MdfTable, dsn string, opts WriteOpts) error {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("mysql open: %w", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("mysql connect: %w", err)
	}
	fmt.Printf("Connected to MySQL\n\n")

	for _, table := range tables {
		if err := mysqlWriteTable(db, table, opts); err != nil {
			fmt.Fprintf(os.Stderr, "  WARNING: %s: %v\n", table.Name, err)
		}
	}
	return nil
}

const mysqlBatchSize = 500

func mysqlWriteTable(db *sql.DB, table *mdf.MdfTable, opts WriteOpts) error {
	cols := table.ActiveColumns()
	if len(cols) == 0 {
		fmt.Printf("  %-40s  (no columns, skipped)\n", table.Name)
		return nil
	}

	if opts.DropFirst {
		if _, err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", mysqlQuoteIdent(table.Name))); err != nil {
			return fmt.Errorf("drop table: %w", err)
		}
	}
	if opts.CreateTables {
		ddl := mysqlBuildDDL(table.Name, cols)
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}

	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = mysqlQuoteIdent(c.Name)
	}

	placeholders := "(" + strings.Repeat("?,", len(cols)-1) + "?)"
	baseInsert := fmt.Sprintf("INSERT INTO %s (%s) VALUES ",
		mysqlQuoteIdent(table.Name),
		strings.Join(colNames, ","))

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
		query := baseInsert + strings.Join(rowPlaceholders, ",")
		if _, err := db.Exec(query, args...); err != nil {
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
			args[i] = sqlValueToSQL(v)
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

// mysqlBuildDDL generates a CREATE TABLE IF NOT EXISTS for MySQL.
func mysqlBuildDDL(tableName string, cols []mdf.ColumnType) string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE IF NOT EXISTS ")
	sb.WriteString(mysqlQuoteIdent(tableName))
	sb.WriteString(" (\n")
	for i, col := range cols {
		sb.WriteString("  ")
		sb.WriteString(mysqlQuoteIdent(col.Name))
		sb.WriteString(" ")
		sb.WriteString(mysqlSQLType(col.DataType))
		if col.Nullable {
			sb.WriteString(" NULL")
		} else {
			sb.WriteString(" NOT NULL")
		}
		if i < len(cols)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(")")
	return sb.String()
}

// mysqlSQLType maps an MDF SqlType to a MySQL type name.
func mysqlSQLType(t mdf.SqlType) string {
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
		return "DOUBLE"
	case mdf.SqlBit:
		return "TINYINT(1)"
	case mdf.SqlDateTime, mdf.SqlSmallDateTime:
		return "DATETIME(3)"
	case mdf.SqlUniqueIdentifier:
		return "VARCHAR(36)"
	case mdf.SqlVarChar, mdf.SqlChar, mdf.SqlNVarChar, mdf.SqlNChar, mdf.SqlSysName:
		return "LONGTEXT CHARACTER SET utf8mb4"
	default:
		return "LONGBLOB"
	}
}

// mysqlQuoteIdent wraps a name in backticks.
func mysqlQuoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// sqlValueToSQL converts an SqlValue to a Go value accepted by database/sql.
// Shared by MySQL and MSSQL drivers.
func sqlValueToSQL(v *mdf.SqlValue) any {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case mdf.SqlTinyInt, mdf.SqlSmallInt, mdf.SqlInt, mdf.SqlBigInt:
		return v.IntVal
	case mdf.SqlFloat:
		return v.FloatVal
	case mdf.SqlBit:
		if v.BoolVal {
			return int64(1)
		}
		return int64(0)
	case mdf.SqlDateTime, mdf.SqlSmallDateTime:
		return v.TimeVal
	case mdf.SqlUniqueIdentifier:
		return v.StrVal
	case mdf.SqlVarChar, mdf.SqlChar, mdf.SqlNVarChar, mdf.SqlNChar, mdf.SqlSysName:
		return strings.TrimRight(v.StrVal, "\x00")
	default:
		return v.BlobVal
	}
}
