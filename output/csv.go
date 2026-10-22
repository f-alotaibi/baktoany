package output

import (
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/f-alotaibi/baktoany/mdf"
)

// ExportToCSV writes each table to a separate .csv file in dir.
// One file per table named <TableName>.csv with a header row.
func ExportToCSV(tables []*mdf.MdfTable, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	for _, table := range tables {
		if err := exportTableCSV(table, dir); err != nil {
			fmt.Fprintf(os.Stderr, "  WARNING: %s: %v\n", table.Name, err)
		}
	}
	return nil
}

func exportTableCSV(table *mdf.MdfTable, dir string) error {
	path := filepath.Join(dir, table.Name+".csv")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)

	// Header
	cols := table.ActiveColumns()
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.Name
	}
	if err := w.Write(header); err != nil {
		return err
	}

	var rows, errs int64
	table.EachRow(func(row []*mdf.SqlValue) bool {
		record := make([]string, len(cols))
		for i, v := range row {
			if i < len(cols) && !cols[i].Computed {
				record[i] = sqlValueToCSV(v)
			}
		}
		if err := w.Write(record); err != nil {
			errs++
		} else {
			rows++
		}
		return true
	})

	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}

	fmt.Printf("  %-40s %8d rows  →  %s\n", table.Name, rows, path)
	if errs > 0 {
		fmt.Fprintf(os.Stderr, "    %d write errors\n", errs)
	}
	return nil
}

// sqlValueToCSV converts a SqlValue to its CSV string representation.
// nil → empty string (can change to \N for psql COPY compatibility)
func sqlValueToCSV(v *mdf.SqlValue) string {
	if v == nil {
		return ""
	}
	switch v.Kind {
	case mdf.SqlTinyInt, mdf.SqlSmallInt, mdf.SqlInt, mdf.SqlBigInt:
		return fmt.Sprintf("%d", v.IntVal)
	case mdf.SqlFloat:
		return fmt.Sprintf("%g", v.FloatVal)
	case mdf.SqlBit:
		if v.BoolVal {
			return "true"
		}
		return "false"
	case mdf.SqlDateTime, mdf.SqlSmallDateTime:
		return v.TimeVal.UTC().Format("2006-01-02 15:04:05.000")
	case mdf.SqlUniqueIdentifier:
		return v.StrVal
	case mdf.SqlVarChar, mdf.SqlChar, mdf.SqlNVarChar, mdf.SqlNChar, mdf.SqlSysName:
		return strings.TrimRight(v.StrVal, "\x00")
	case mdf.SqlVarBinary, mdf.SqlBinary, mdf.SqlImage, mdf.SqlNText, mdf.SqlVariant:
		if v.BlobVal == nil {
			return ""
		}
		return "0x" + hex.EncodeToString(v.BlobVal)
	default:
		if v.StrVal != "" {
			return v.StrVal
		}
		if v.BlobVal != nil {
			return "0x" + hex.EncodeToString(v.BlobVal)
		}
		return fmt.Sprintf("%d", v.IntVal)
	}
}

// FormatTimestamp formats a time.Time as a SQL-compatible timestamp string.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000")
}
