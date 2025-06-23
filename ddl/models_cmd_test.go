package ddl

import (
	"bytes"
	"database/sql"
	"github.com/blink-io/sqddl/internal/testutil"
	"io"
	"os"
	"strings"
	"testing"
)

func TestModelsCmd(t *testing.T) {
	t.Parallel()
	dsn := "file:/" + t.Name() + ".db?vfs=memdb&_foreign_keys=true"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	defer db.Close()

	migrateCmd, err := MigrateCommand("-db", dsn, "-dir", "sqlite_migrations")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	migrateCmd.Stderr = io.Discard
	migrateCmd.db = "" // Keep database open after running command.
	defer migrateCmd.DB.Close()
	err = migrateCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}

	buf := &bytes.Buffer{}
	modelsCmd, err := ModelsCommand("-db", dsn, "-pkg", "sakila")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	modelsCmd.Stdout = buf
	modelsCmd.db = "" // Keep database open after running command.
	defer closeQuietly(modelsCmd.DB.Close)
	err = modelsCmd.Run()
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
