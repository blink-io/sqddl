package ddl

import (
	"database/sql"
	"io"
	"os"
	"testing"

	"github.com/blink-io/sqddl/internal/testutil"
)

func TestLsCmd(t *testing.T) {
	t.Run("empty db", func(t *testing.T) {
		t.Parallel()
		dsn := "file:/" + t.Name() + ".db?vfs=memdb&_foreign_keys=true"
		// Assert OK.
		lsCmd, err := LsCommand("-db", dsn, "-dir", "sqlite_migrations")
		if err != nil {
			t.Error(testutil.Callers(), err)
		}
		lsCmd.Stdout = io.Discard
		err = lsCmd.Run()
		if err != nil {
			t.Error(testutil.Callers(), err)
		}
	})

	t.Run("non-empty db", func(t *testing.T) {
		t.Parallel()
		db, err := sql.Open("sqlite3", "file:/"+t.Name()+".db?vfs=memdb&_foreign_keys=true")
		if err != nil {
			t.Fatal(testutil.Callers(), err)
		}
		// Run the migrations.
		migrateCmd := &MigrateCmd{
			DB:      db,
			Dialect: "sqlite",
			DirFS:   os.DirFS("sqlite_migrations"),
			Stderr:  io.Discard,
		}
		err = migrateCmd.Run()
		if err != nil {
			t.Fatal(testutil.Callers(), err)
		}
		// Assert OK.
		lsCmd := &LsCmd{
			DB:      db,
			Dialect: "sqlite",
			DirFS:   os.DirFS("sqlite_migrations"),
			Stdout:  io.Discard,
		}
		err = lsCmd.Run()
		if err != nil {
			t.Error(testutil.Callers(), err)
		}
	})
}
