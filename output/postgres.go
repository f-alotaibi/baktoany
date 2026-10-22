package output

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/f-alotaibi/baktoany/mdf"
)

// ExportToPostgres connects to PostgreSQL and restores all tables using
// the fast COPY FROM protocol (streaming, no full load into RAM).
func ExportToPostgres(tables []*mdf.MdfTable, connStr string, opts WriteOpts) error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return fmt.Errorf("postgres connect: %w", err)
	}
	defer conn.Close(ctx)

	fmt.Printf("Connected to PostgreSQL\n\n")

	for _, table := range tables {
		if err := pgWriteTable(ctx, conn, table, opts); err != nil {
			fmt.Fprintf(os.Stderr, "  WARNING: %s: %v\n", table.Name, err)
		}
	}
	return nil
}

func pgWriteTable(ctx context.Context, conn *pgx.Conn, table *mdf.MdfTable, opts WriteOpts) error {
	cols := table.ActiveColumns()
	if len(cols) == 0 {
		fmt.Printf("  %-40s  (no columns, skipped)\n", table.Name)
		return nil
	}

	if opts.DropFirst {
		_, err := conn.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, pgQuoteIdent(table.Name)))
		if err != nil {
			return fmt.Errorf("drop table: %w", err)
		}
	}

	if opts.CreateTables {
		ddl := pgBuildDDL(table.Name, cols)
		if _, err := conn.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}

	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = c.Name
	}

	src := newPgCopySource(table, cols)
	count, err := conn.CopyFrom(ctx, pgx.Identifier{table.Name}, colNames, src)
	if err != nil {
		return fmt.Errorf("COPY: %w", err)
	}
	fmt.Printf("  %-40s %8d rows\n", table.Name, count)
	return nil
}

// pgCopySource implements pgx.CopyFromSource, streaming rows from an MdfTable.
type pgCopySource struct {
	ch   chan []*mdf.SqlValue
	curr []*mdf.SqlValue
	cols []mdf.ColumnType
	err  error
}

func newPgCopySource(table *mdf.MdfTable, cols []mdf.ColumnType) *pgCopySource {
	ch := make(chan []*mdf.SqlValue, 256)
	src := &pgCopySource{ch: ch, cols: cols}
	go func() {
		defer close(ch)
		table.EachRow(func(row []*mdf.SqlValue) bool {
			ch <- row
			return true
		})
	}()
	return src
}

func (s *pgCopySource) Next() bool {
	row, ok := <-s.ch
	if !ok {
		return false
	}
	s.curr = row
	return true
}

func (s *pgCopySource) Values() ([]any, error) {
	vals := make([]any, len(s.cols))
	for i, col := range s.cols {
		// Find matching value index (cols are a subset if computed cols were dropped)
		var v *mdf.SqlValue
		if i < len(s.curr) {
			v = s.curr[i]
		}
		vals[i] = sqlValueToPg(v, col.DataType)
	}
	return vals, nil
}

func (s *pgCopySource) Err() error { return s.err }

// sqlValueToPg converts an SqlValue to the appropriate Go type for pgx.
func sqlValueToPg(v *mdf.SqlValue, typ mdf.SqlType) any {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case mdf.SqlTinyInt, mdf.SqlSmallInt:
		return int16(v.IntVal)
	case mdf.SqlInt:
		return int32(v.IntVal)
	case mdf.SqlBigInt:
		return v.IntVal
	case mdf.SqlFloat:
		return v.FloatVal
	case mdf.SqlBit:
		return v.BoolVal
	case mdf.SqlDateTime, mdf.SqlSmallDateTime:
		return v.TimeVal
	case mdf.SqlUniqueIdentifier:
		return v.StrVal // pgx handles UUID strings fine
	case mdf.SqlVarChar, mdf.SqlChar, mdf.SqlNVarChar, mdf.SqlNChar, mdf.SqlSysName:
		return strings.TrimRight(v.StrVal, "\x00")
	default:
		return v.BlobVal // BYTEA
	}
}

// pgBuildDDL generates a CREATE TABLE IF NOT EXISTS statement for PostgreSQL.
func pgBuildDDL(tableName string, cols []mdf.ColumnType) string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE IF NOT EXISTS ")
	sb.WriteString(pgQuoteIdent(tableName))
	sb.WriteString(" (\n")
	for i, col := range cols {
		sb.WriteString("  ")
		sb.WriteString(pgQuoteIdent(col.Name))
		sb.WriteString(" ")
		sb.WriteString(pgSQLType(col.DataType))
		if i < len(cols)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(")")
	return sb.String()
}

// pgSQLType maps an MDF SqlType to a PostgreSQL type name.
func pgSQLType(t mdf.SqlType) string {
	switch t.Kind {
	case mdf.SqlTinyInt, mdf.SqlSmallInt:
		return "SMALLINT"
	case mdf.SqlInt:
		return "INTEGER"
	case mdf.SqlBigInt:
		return "BIGINT"
	case mdf.SqlFloat:
		return "DOUBLE PRECISION"
	case mdf.SqlBit:
		return "BOOLEAN"
	case mdf.SqlDateTime, mdf.SqlSmallDateTime:
		return "TIMESTAMP"
	case mdf.SqlUniqueIdentifier:
		return "VARCHAR(36)"
	case mdf.SqlVarChar, mdf.SqlChar, mdf.SqlNVarChar, mdf.SqlNChar, mdf.SqlSysName:
		return "TEXT"
	default:
		return "BYTEA"
	}
}

// pgQuoteIdent wraps a name in double-quotes, escaping any internal double-quotes.
func pgQuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// WriteOpts controls table creation behaviour.
type WriteOpts struct {
	CreateTables bool
	DropFirst    bool
}
