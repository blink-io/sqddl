package ddl

import (
	"database/sql"
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/blink-io/sqddl/internal/testutil"
)

func TestTouchCmd(t *testing.T) {
	t.Parallel()
	dsn := "file:/" + t.Name() + ".db?vfs=memdb&_foreign_keys=true"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}

	assertHistoryTable := func(t *testing.T, db *sql.DB, wantFilenames ...string) {
		var gotFilenames []string
		rows, err := db.Query("SELECT filename FROM sqddl_history ORDER BY filename")
		if err != nil {
			t.Fatal(testutil.Callers(), err)
		}
		defer rows.Close()
		for rows.Next() {
			var filename string
			err := rows.Scan(&filename)
			if err != nil {
				t.Fatal(testutil.Callers(), err)
			}
			gotFilenames = append(gotFilenames, filename)
		}
		err = rows.Close()
		if err != nil {
			t.Fatal(testutil.Callers(), err)
		}
		if diff := testutil.Diff(gotFilenames, wantFilenames); diff != "" {
			t.Fatal(testutil.Callers(), diff)
		}
	}

	// Touch customer_list.sql, film_list.sql.
	touchCmd, err := TouchCommand(
		"-db", dsn,
		"-dir", "sqlite_migrations",
		"sqlite_migrations/repeatable/views/customer_list.sql",
		"repeatable/views/film_list.sql",
	)
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	touchCmd.Stderr = io.Discard
	touchCmd.db = "" // Keep database open after running command.
	defer touchCmd.DB.Close()
	err = touchCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}

	// Assert filenames in history table.
	assertHistoryTable(t, touchCmd.DB,
		"repeatable/views/customer_list.sql",
		"repeatable/views/film_list.sql",
	)

	// Touch customer_list.sql, staff_list.sql.
	touchCmd, err = TouchCommand(
		"-db", dsn,
		"-dir", "sqlite_migrations",
		"repeatable/views/customer_list.sql",
		"sqlite_migrations/repeatable/views/staff_list.sql",
	)
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	touchCmd.Stderr = io.Discard
	touchCmd.db = "" // Keep database open after running command.
	defer touchCmd.DB.Close()
	err = touchCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}

	// Assert filenames in history table.
	assertHistoryTable(t, db,
		"repeatable/views/customer_list.sql",
		"repeatable/views/film_list.sql",
		"repeatable/views/staff_list.sql",
	)

	// Touch everything in the sqlite_migrations/repeatable/views directory.
	touchCmd = &TouchCmd{
		DB:      db,
		Dialect: "sqlite",
		DirFS:   os.DirFS("sqlite_migrations"),
		Stderr:  io.Discard,
	}
	touchCmd.Filenames, err = fs.Glob(touchCmd.DirFS, "repeatable/views/*")
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}
	err = touchCmd.Run()
	if err != nil {
		t.Fatal(testutil.Callers(), err)
	}

	// Assert filenames in history table.
	assertHistoryTable(t, db,
		"repeatable/views/actor_info.sql",
		"repeatable/views/customer_list.sql",
		"repeatable/views/film_list.sql",
		"repeatable/views/full_address.sql",
		"repeatable/views/nicer_but_slower_film_list.sql",
		"repeatable/views/sales_by_film_category.sql",
		"repeatable/views/sales_by_store.sql",
		"repeatable/views/staff_list.sql",
	)
}
