package ddl

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/blink-io/sqddl/internal/testutil"
	"github.com/stretchr/testify/require"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestModelsCmd(t *testing.T) {
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
	migrateCmd.db = "" // Keep the database open after running command.
	defer closeQuietly(migrateCmd.DB.Close)
	err = migrateCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}

	buf := &bytes.Buffer{}
	modelsCmd, err := ModelsCommand("-db", dsn, "-pkg", "orm")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	modelsCmd.Stdout = buf
	modelsCmd.db = "" // Keep the database open after running command.
	defer closeQuietly(modelsCmd.DB.Close)
	err = modelsCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	b, err := os.ReadFile("testdata/sqlite/models.go.txt")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	wantOutput := strings.ReplaceAll(string(b), "\r\n", "\n")
	gotOutput := buf.String()
	if diff := testutil.Diff(gotOutput, wantOutput); diff != "" {
		t.Error(testutil.Callers(), diff)
	}
}

func TestModelsCmd_Postgres(t *testing.T) {
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
	modelsCmd, err := ModelsCommand("-db", dsn, "-pkg", "sqtest", "-schemas", "public", "-tables", "tbl_basic")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	modelsCmd.Stdout = buf
	modelsCmd.db = "" // Keep the database open after running command.
	defer closeQuietly(modelsCmd.DB.Close)
	err = modelsCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	b, err := os.ReadFile("testdata/sqlite/models.go.txt")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	wantOutput := strings.ReplaceAll(string(b), "\r\n", "\n")
	gotOutput := buf.String()
	if diff := testutil.Diff(gotOutput, wantOutput); diff != "" {
		t.Error(testutil.Callers(), diff)
	}
}

func TestRegex_1(t *testing.T) {
	rr, err := regexp.Compile("(_)?([A-Z])*ID$")
	require.NoError(t, err)
	strs := []string{
		"X_ID",
		"X_SSID",
		"S_ID_",
		"UN_KK_ID",
		"x_zid",
		"x_iid",
		"XXID",
		"UID",
		"KUIDS",
	}
	for _, str := range strs {
		fmt.Printf("%v\n", rr.MatchString(str))
	}
}
