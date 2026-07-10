// Command dbq runs one read-only SQL query against a sesh store database and
// prints result rows tab-separated, one per line. It exists for the scenario
// gate harnesses (tests/check-s*.sh): gate machines are not assumed to carry
// a sqlite3 CLI, and the store database must be inspected with the same
// driver the store writes with.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"

	"sesh/internal/sqlitedsn"
)

func main() {
	dbPath := flag.String("db", "", "path to store.sqlite")
	flag.Parse()
	if *dbPath == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: dbq -db <store.sqlite> '<select statement>'")
		os.Exit(2)
	}
	if err := run(*dbPath, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "dbq:", err)
		os.Exit(1)
	}
}

func run(dbPath, query string) error {
	// mode=ro: assertions must never mutate store state; busy_timeout rides
	// out a store holding the WAL write lock mid-transaction.
	db, err := sql.Open("sqlite", sqlitedsn.ReadOnly(dbPath))
	if err != nil {
		return err
	}
	defer db.Close()

	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	vals := make([]any, len(cols))
	for i := range vals {
		vals[i] = new(sql.NullString)
	}
	for rows.Next() {
		if err := rows.Scan(vals...); err != nil {
			return err
		}
		out := make([]string, len(cols))
		for i, v := range vals {
			ns := v.(*sql.NullString)
			if ns.Valid {
				out[i] = ns.String
			} else {
				out[i] = "NULL"
			}
		}
		fmt.Println(strings.Join(out, "\t"))
	}
	return rows.Err()
}
