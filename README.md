# baktoany

[![CI & Release](https://github.com/f-alotaibi/baktoany/actions/workflows/ci.yml/badge.svg)](https://github.com/f-alotaibi/baktoany/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/f-alotaibi/baktoany)](https://goreportcard.com/report/github.com/f-alotaibi/baktoany)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**baktoany** is a standalone, high-performance CLI utility written in Go that extracts and restores Microsoft SQL Server backup files (`.BAK`) **without requiring Microsoft SQL Server** to be installed or running.

It directly parses Microsoft Tape Format (MTF) backup containers, interprets underlying MDF/database data pages, schemas, and catalogs, and streams the extracted data directly into your destination of choice: **PostgreSQL**, **MySQL / MariaDB**, **SQL Server**, or **CSV** files.

---

## Features

- **Zero SQL Server Dependency:** You don't need SQL Server, Docker containers, or Windows VMs just to read or migrate data from a `.BAK` file.
- **Direct Multi-Target Migration:**
  - **PostgreSQL** (via `pgx`)
  - **MySQL / MariaDB** (via `go-sql-driver/mysql`)
  - **Microsoft SQL Server** (native cross-instance restore via `go-mssqldb`)
  - **CSV files** (one file per extracted table)
- **Schema & Data Restoration:** Automatically creates target tables with matching data types or imports into pre-existing schemas.
- **Selective Extraction:** Filter specific tables to extract using `-t` or inspect contents using `--list`.
- **Fast & Memory Efficient:** Reads page headers and indices dynamically from the backup stream without loading the full file into memory.
- **Cross-Platform:** Single self-contained binary for Linux, macOS, and Windows.

---

## Installation

### Pre-built Binaries

Download pre-compiled binaries for your architecture from the [GitHub Releases](https://github.com/f-alotaibi/baktoany/releases) page.

| OS | Architecture | Binary |
|---|---|---|
| Linux | x86_64 / amd64 | `baktoany-linux-amd64` |
| Linux | ARM64 | `baktoany-linux-arm64` |
| macOS | Intel (amd64) | `baktoany-darwin-amd64` |
| macOS | Apple Silicon (arm64) | `baktoany-darwin-arm64` |
| Windows | x86_64 | `baktoany-windows-amd64.exe` |

### From Source

Requires **Go 1.22+**:

```bash
git clone https://github.com/f-alotaibi/baktoany.git
cd baktoany
go build -ldflags="-s -w" -o baktoany .
```

---

## Usage

```bash
baktoany <backup.bak> [options]
```

### Options

| Flag | Description |
|---|---|
| `--list` | List all tables (system and user tables) and columns in the `.BAK` file and exit |
| `-t <tables>` | Comma-separated list of table names to extract (default: all user tables) |
| `--postgres <conn>` | PostgreSQL connection URL, e.g. `postgres://user:pass@localhost:5432/mydb` |
| `--mysql <dsn>` | MySQL DSN, e.g. `user:pass@tcp(localhost:3306)/mydb` |
| `--mssql <conn>` | SQL Server connection URL, e.g. `sqlserver://user:pass@host:1433?database=mydb` |
| `--csv <dir>` | Directory path to export tables as individual CSV files |
| `--create-tables` | Create tables in destination database if they do not exist (default: `true`) |
| `--drop` | Drop existing tables in target database before creating them (default: `false`) |
| `--include-empty` | Include empty tables (without data pages) when creating schema or exporting (default: `false`) |

---

## Examples

### 1. Inspect Tables in a Backup
List all user and system tables without running any migration:

```bash
baktoany database.bak --list
```

### 2. Export Directly to PostgreSQL
```bash
baktoany database.bak --postgres "postgres://postgres:secret@localhost:5432/my_pg_db"
```

### 3. Export Directly to MySQL
```bash
baktoany database.bak --mysql "root:password@tcp(127.0.0.1:3306)/target_db"
```

### 4. Export to CSV
Extract all user tables into CSV files under `./exports`:

```bash
mkdir -p ./exports
baktoany database.bak --csv ./exports
```

### 5. Extract Specific Tables Only
Extract only the `Customers` and `Orders` tables and recreate them in PostgreSQL:

```bash
baktoany database.bak -t Customers,Orders --drop --postgres "postgres://user:pass@localhost:5432/mydb"
```

---

## How It Works

1. **MTF Stream Detection:** Scans the `.BAK` file structure for Microsoft Tape Format (MTF) descriptors and locates the primary database stream (`MQDA`).
2. **Page Indexing:** Indexes SQL Server 8KB database pages (`data`, `index`, `allocation map / GAM / SGAM / IAM`, and system metadata).
3. **Catalog Parsing:** Reads system schemas (`sys.sysschobjs`, `sys.syscolpars`, etc.) to reconstruct table structures, columns, and types.
4. **Data Deserialization & Streaming:** Parses slot arrays, row data, and variable-length columns, translating values and piping them in batches directly into target databases or CSV writers.


# Creds
BAK-SHELL: Original implementation using C#
mdf-rs: Rust implementation (used for inspiration and reference)