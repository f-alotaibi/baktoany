package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/f-alotaibi/baktoany/mdf"
	"github.com/f-alotaibi/baktoany/mtf"
	"github.com/f-alotaibi/baktoany/output"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		listOnly     = flag.Bool("list", false, "List tables and exit")
		tableFilter  = flag.String("t", "", "Comma-separated table names to extract (default: all user tables)")
		postgresConn = flag.String("postgres", "", `PostgreSQL connection string, e.g. "postgres://user:pass@host/dbname"`)
		mysqlDSN     = flag.String("mysql", "", `MySQL DSN, e.g. "user:pass@tcp(host:3306)/dbname"`)
		mssqlConn    = flag.String("mssql", "", `SQL Server connection string, e.g. "sqlserver://user:pass@host?database=db"`)
		csvDir       = flag.String("csv", "", "Directory to write CSV files into")
		createTables = flag.Bool("create-tables", true, "Create tables in the target database if they don't exist")
		dropFirst    = flag.Bool("drop", false, "Drop existing tables before creating them")
	)
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		usage()
		return fmt.Errorf("no .BAK file specified")
	}
	bakPath := args[0]

	if _, err := os.Stat(bakPath); err != nil {
		return fmt.Errorf("cannot open backup file: %w", err)
	}

	// ── Step 1: Find the MQDA stream ──────────────────────────────────────────
	fmt.Printf("=== baktoany ===\n")
	fmt.Printf("Backup: %s\n\n", bakPath)
	fmt.Println("Parsing backup file (MTF)...")

	mqdaOffset, mqdaLength, found, err := mtf.FindMqdaStream(bakPath)
	if err != nil {
		return fmt.Errorf("MTF scan: %w", err)
	}
	if !found {
		return fmt.Errorf("no MQDA (database) stream found — is this a valid SQL Server .BAK file?")
	}
	fmt.Printf("  Found MQDA stream (%s)\n", formatBytes(mqdaLength))

	// ── Step 2: Build the page index ──────────────────────────────────────────
	fmt.Println("  Building page index...")
	t0 := time.Now()

	pp, err := mtf.NewPageProvider(bakPath, mqdaOffset, mqdaLength)
	if err != nil {
		return fmt.Errorf("page provider: %w", err)
	}
	defer pp.Close()
	fmt.Printf("  Indexed %d pages in %.1fs\n", pp.PageCount(), time.Since(t0).Seconds())

	// ── Step 3: Read the database catalog ─────────────────────────────────────
	fmt.Println("  Parsing database catalog...")
	db, err := mdf.OpenDatabase(pp)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	userTables := db.UserTables()
	allTables := db.AllTables()
	fmt.Printf("\nFound %d tables (%d user tables)\n\n", len(allTables), len(userTables))

	// ── List mode ─────────────────────────────────────────────────────────────
	if *listOnly {
		fmt.Printf("%-12s  %-40s  %s\n", "Type", "Table", "Columns")
		fmt.Println(strings.Repeat("-", 65))
		for _, obj := range allTables {
			typeName := "SystemTable"
			if obj.Type == mdf.SchTypeUserTable {
				typeName = "UserTable"
			}
			cols := db.ColumnsForTable(obj)
			fmt.Printf("%-12s  %-40s  %d\n", typeName, obj.Name, len(cols))
		}
		return nil
	}

	// ── Validate that at least one output target is specified ─────────────────
	if *postgresConn == "" && *mysqlDSN == "" && *mssqlConn == "" && *csvDir == "" {
		return fmt.Errorf("no output target specified — use --postgres, --mysql, --mssql, or --csv\n" +
			"Run with --list to see available tables.")
	}

	// ── Filter tables ─────────────────────────────────────────────────────────
	var selected []SysSchObjEntry
	if *tableFilter != "" {
		names := splitAndTrim(*tableFilter)
		nameSet := make(map[string]bool)
		for _, n := range names {
			nameSet[strings.ToLower(n)] = true
		}
		for _, obj := range userTables {
			if nameSet[strings.ToLower(obj.Name)] {
				selected = append(selected, obj)
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("no tables matched filter: %s", *tableFilter)
		}
	} else {
		selected = userTables
	}

	// ── Build MdfTable objects ─────────────────────────────────────────────────
	var tables []*mdf.MdfTable
	for _, obj := range selected {
		tbl, err := db.BuildTable(obj)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: cannot build table %s: %v\n", obj.Name, err)
			continue
		}
		if len(tbl.Partitions) == 0 {
			fmt.Printf("  %-40s  (no data pages)\n", obj.Name)
			continue
		}
		tables = append(tables, tbl)
	}

	if len(tables) == 0 {
		return fmt.Errorf("no tables with data pages found")
	}

	opts := output.WriteOpts{
		CreateTables: *createTables,
		DropFirst:    *dropFirst,
	}

	tStart := time.Now()

	// ── CSV ───────────────────────────────────────────────────────────────────
	if *csvDir != "" {
		fmt.Printf("Exporting to CSV → %s\n", *csvDir)
		if err := output.ExportToCSV(tables, *csvDir); err != nil {
			return fmt.Errorf("csv: %w", err)
		}
	}

	// ── PostgreSQL ────────────────────────────────────────────────────────────
	if *postgresConn != "" {
		fmt.Printf("Restoring to PostgreSQL...\n")
		if err := output.ExportToPostgres(tables, *postgresConn, opts); err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
	}

	// ── MySQL ─────────────────────────────────────────────────────────────────
	if *mysqlDSN != "" {
		fmt.Printf("Restoring to MySQL...\n")
		if err := output.ExportToMySQL(tables, *mysqlDSN, opts); err != nil {
			return fmt.Errorf("mysql: %w", err)
		}
	}

	// ── SQL Server ────────────────────────────────────────────────────────────
	if *mssqlConn != "" {
		fmt.Printf("Restoring to SQL Server...\n")
		if err := output.ExportToMSSQL(tables, *mssqlConn, opts); err != nil {
			return fmt.Errorf("mssql: %w", err)
		}
	}

	fmt.Printf("\n=== Done in %.1fs ===\n", time.Since(tStart).Seconds())
	return nil
}

// SysSchObjEntry is a local alias to avoid importing mdf in the type list.
type SysSchObjEntry = mdf.SysSchObj

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `baktoany — restore SQL Server .BAK files without SQL Server

Usage:
  baktoany <backup.bak> [options]

Output targets (at least one required unless --list):
  --postgres <conn>    PostgreSQL connection string
                         e.g. "postgres://user:pass@localhost/mydb"
  --mysql <dsn>        MySQL/MariaDB DSN
                         e.g. "user:pass@tcp(localhost:3306)/mydb"
  --mssql <conn>       SQL Server connection string
                         e.g. "sqlserver://user:pass@host?database=mydb"
  --csv <dir>          Export each table as <dir>/<TableName>.csv

Table selection:
  --list               List all tables in the backup and exit
  -t <names>           Comma-separated table names to extract
                         (default: all user tables)

Table creation:
  --create-tables      Create tables in target DB (default: true)
  --drop               Drop existing tables before creating (default: false)

Examples:
  baktoany backup.bak --list
  baktoany backup.bak --postgres "postgres://admin:pass@localhost/restore"
  baktoany backup.bak --mysql "root:pass@tcp(localhost)/mydb"
  baktoany backup.bak --mssql "sqlserver://sa:pass@localhost?database=mydb"
  baktoany backup.bak --csv ./output
  baktoany backup.bak -t Company,Orders --postgres "postgres://..."
  baktoany backup.bak -t Customers --drop --mssql "sqlserver://..."

`)
}
