package ddl

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/blink-io/sqddl/internal/testutil"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestTablesCmd(t *testing.T) {
	t.Parallel()
	dsn := "file:/" + t.Name() + ".db?vfs=memdb&_foreign_keys=true"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	defer closeQuietly(db.Close)

	migrateCmd, err := MigrateCommand("-db", dsn, "-dir", "sqlite_migrations")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	migrateCmd.Stderr = io.Discard
	migrateCmd.db = "" // Keep database open after running command.
	defer closeQuietly(migrateCmd.DB.Close)
	err = migrateCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}

	buf := &bytes.Buffer{}
	tablesCmd, err := TablesCommand("-db", dsn, "-pkg", "sakila")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	tablesCmd.Stdout = buf
	tablesCmd.db = "" // Keep the database open after running command.
	defer closeQuietly(tablesCmd.DB.Close)
	err = tablesCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	b, err := os.ReadFile("testdata/sqlite/tables.go.txt")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	wantOutput := strings.ReplaceAll(string(b), "\r\n", "\n")
	gotOutput := buf.String()
	if diff := testutil.Diff(gotOutput, wantOutput); diff != "" {
		t.Error(testutil.Callers(), diff)
	}
}

func TestTablesCmd_Postgres(t *testing.T) {
	t.Parallel()
	dsn := "postgres://test:test@localhost:15432/test?sslmode=disable"
	//dsn := "postgres://test:test@192.168.50.88:5432/test?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	defer closeQuietly(db.Close)

	//migrateCmd, err := MigrateCommand("-db", dsn, "-dir", "sqlite_migrations")
	//if err != nil {
	//	t.Fatal(testutil.Callers(), err)
	//}
	//migrateCmd.Stderr = io.Discard
	//migrateCmd.db = "" // Keep database open after running command.
	//defer closeQuietly(migrateCmd.DB.Close)
	//err = migrateCmd.Run()
	//if err != nil {
	//	t.Fatal(testutil.Callers(), err)
	//}

	buf := &bytes.Buffer{}
	tablesCmd, err := TablesCommand("-db", dsn, "-pkg", "sqtest", "-tables", "tbl_basic")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	tablesCmd.Stdout = buf
	tablesCmd.db = "" // Keep the database open after running command.
	defer closeQuietly(tablesCmd.DB.Close)
	err = tablesCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	b, err := os.ReadFile("testdata/sqlite/tables.go.txt")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	wantOutput := strings.ReplaceAll(string(b), "\r\n", "\n")
	gotOutput := buf.String()
	if diff := testutil.Diff(gotOutput, wantOutput); diff != "" {
		t.Error(testutil.Callers(), diff)
	}
}
