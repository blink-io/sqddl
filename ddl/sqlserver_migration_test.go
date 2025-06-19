package ddl

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blink-io/sqddl/internal/testutil"
)

func Test_sqlserverMigration(t *testing.T) {
	type TT struct {
		dir         string
		dropObjects bool
	}
	tests := []TT{
		{"testdata/sqlserver_schema", true},
		{"testdata/sqlserver_table", true},
		{"testdata/sqlserver_drop", true},
		{"testdata/sqlserver_add", false},
		{"testdata/sqlserver_alter", false},
		{"testdata/sqlserver_ignore", true},
	}
	newCatalog := func(t *testing.T, filename string) *Catalog {
		file, err := os.Open(filename)
		if err != nil {
			t.Fatal(testutil.Callers(), err)
		}
		defer file.Close()
		p := NewStructParser(nil)
		err = p.ParseFile(file)
		if err != nil {
			t.Fatal(testutil.Callers(), err)
		}
		catalog := &Catalog{Dialect: "sqlserver", CurrentSchema: "dbo"}
		err = p.WriteCatalog(catalog)
		if err != nil {
			t.Fatal(testutil.Callers(), err)
		}
		return catalog
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.dir, func(t *testing.T) {
			t.Parallel()
			srcCatalog := newCatalog(t, tt.dir+"/src.go.txt")
			destCatalog := newCatalog(t, tt.dir+"/dest.go.txt")
			m := newSQLServerMigration(srcCatalog, destCatalog, tt.dropObjects)
			filenames, bufs, warnings := m.sql(strings.TrimPrefix(filepath.Base(tt.dir), "sqlserver_"))
			for i, filename := range filenames {
				b, err := os.ReadFile(tt.dir + "/" + filename)
				if err != nil {
					t.Error(testutil.Callers(), err)
					continue
				}
				wantContent := string(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
				gotContent := bufs[i].String()
				if diff := testutil.Diff(gotContent, wantContent); diff != "" {
					t.Error(testutil.Callers(), diff)
				}
			}
			var wantWarnings, gotWarnings string
			b, err := os.ReadFile(tt.dir + "/warnings.txt")
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Error(testutil.Callers(), err)
				return
			}
			wantWarnings = string(bytes.TrimSpace(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))))
			if len(warnings) > 0 {
				gotWarnings = strings.Join(warnings, "\n")
			}
			if diff := testutil.Diff(gotWarnings, wantWarnings); diff != "" {
				t.Error(testutil.Callers(), diff)
			}
		})
	}
}
