package ddl

import (
	"bytes"
	"database/sql"
	"go/token"
	"strconv"
	"strings"
)

// ModelStructs is a slice of ModelStruct.
type ModelStructs []ModelStruct

// ModelStruct represents a model struct.
type ModelStruct struct {
	// Name is the name of the table struct.
	Name string

	// Fields are the model struct fields.
	Fields []StructField
}

// ModelField represents a model field within a model struct.
type ModelField struct {
	// Name is the name of the struct field.
	Name string

	// Type is the type of the struct field.
	Type string

	// NameTag is the value for the "sq" struct tag.
	NameTag string

	// Modifiers are the parsed modifiers for the "db" struct tag.
	Modifiers []Modifier

	// tagPos tracks where in the source code the struct tag appeared in. Used
	// for error reporting.
	tagPos token.Pos
}

// NewModelStructs introspects a database connection and returns a slice of
// TableStructs, each TableStruct corresponding to a table in the database. You
// may narrow down the list of tables by filling in the Schemas,
// ExcludeSchemas, Tables and ExcludeTables fields of the Filter struct. The
// Filter.ObjectTypes field will always be set to []string{"TABLES"}.
func NewModelStructs(dialect string, db *sql.DB, filter Filter) (ModelStructs, error) {
	var modelStructs ModelStructs
	var catalog Catalog
	dbi := &DatabaseIntrospector{
		Filter:  filter,
		Dialect: dialect,
		DB:      db,
	}
	dbi.ObjectTypes = []string{"TABLES"}
	err := dbi.WriteCatalog(&catalog)
	if err != nil {
		return nil, err
	}
	err = modelStructs.ReadCatalog(&catalog)
	if err != nil {
		return nil, err
	}
	return modelStructs, nil
}

// ReadCatalog reads from a catalog and populates the ModelStructs accordingly.
func (s *ModelStructs) ReadCatalog(catalog *Catalog) error {
	buf := bufpool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufpool.Put(buf)
	for _, schema := range catalog.Schemas {
		for _, table := range schema.Tables {
			modelStruct := ModelStruct{
				Name:   strings.ToUpper(strings.ReplaceAll(table.TableName, " ", "_")),
				Fields: make([]StructField, 0, len(table.Columns)+1),
			}
			for i, column := range table.Columns {
				if column.Ignore {
					continue
				}
				structField := StructField{
					Name: strings.ToUpper(strings.ReplaceAll(column.ColumnName, " ", "_")),
					Type: getFieldType(catalog.Dialect, &table.Columns[i]),
				}
				if needsQuoting(column.ColumnName) {
					structField.NameTag = column.ColumnName
				}
				var defaultColumnType string
				switch structField.Type {
				case "sq.BinaryField":
					switch catalog.Dialect {
					case DialectSQLite:
						defaultColumnType = "BLOB"
					case DialectPostgres:
						defaultColumnType = "BYTEA"
					case DialectMySQL:
						defaultColumnType = "MEDIUMBLOB"
					case DialectSQLServer:
						defaultColumnType = "VARBINARY(MAX)"
					default:
						defaultColumnType = "BINARY"
					}
				case "sq.BooleanField":
					switch catalog.Dialect {
					case DialectSQLServer:
						defaultColumnType = "BIT"
					default:
						defaultColumnType = "BOOLEAN"
					}
				case "sq.EnumField":
					switch catalog.Dialect {
					case DialectSQLite, DialectPostgres:
						defaultColumnType = "TEXT"
					case DialectSQLServer:
						defaultColumnType = "NVARCHAR(255)"
					default:
						defaultColumnType = "VARCHAR(255)"
					}
				case "sq.JSONField":
					switch catalog.Dialect {
					case DialectSQLite, DialectMySQL:
						defaultColumnType = "JSON"
					case DialectPostgres:
						defaultColumnType = "JSONB"
					case DialectSQLServer:
						defaultColumnType = "NVARCHAR(MAX)"
					default:
						defaultColumnType = "VARCHAR(255)"
					}
				case "sq.NumberField":
					defaultColumnType = "INT"
				case "sq.StringField":
					switch catalog.Dialect {
					case DialectSQLite, DialectPostgres:
						defaultColumnType = "TEXT"
					case DialectSQLServer:
						defaultColumnType = "NVARCHAR(255)"
					default:
						defaultColumnType = "VARCHAR(255)"
					}
				case "sq.TimeField":
					switch catalog.Dialect {
					case DialectPostgres:
						defaultColumnType = "TIMESTAMPTZ"
					case DialectSQLServer:
						defaultColumnType = "DATETIMEOFFSET"
					default:
						defaultColumnType = "TIMESTAMP"
					}
				case "sq.UUIDField":
					switch catalog.Dialect {
					case DialectSQLite, DialectPostgres:
						defaultColumnType = "UUID"
					default:
						defaultColumnType = "BINARY(16)"
					}
				}
				_ = defaultColumnType
				// notnull
				if column.IsNotNull {
					structField.Modifiers = append(structField.Modifiers, Modifier{Name: "notnull"})
				}
				modelStruct.Fields = append(modelStruct.Fields, structField)
			}
			*s = append(*s, modelStruct)
		}
	}
	return nil
}

// MarshalText converts the ModelStructs into Go source code.
func (s *ModelStructs) MarshalText() (text []byte, err error) {
	buf := bufpool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufpool.Put(buf)
	for _, modelStruct := range *s {
		hasColumn := false
		for i := len(modelStruct.Fields) - 1; i >= 0; i-- {
			if modelStruct.Fields[i].Name != "" && modelStruct.Fields[i].Name != "_" {
				hasColumn = true
				break
			}
		}
		if !hasColumn {
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString("type " + modelStruct.Name + " struct {")
		for _, structField := range modelStruct.Fields {
			if structField.Name != "" {
				buf.WriteString("\n\t" + structField.Name + " " + structField.Type)
			} else {
				buf.WriteString("\n\t" + structField.Type)
			}
			ddlTag := Modifiers(structField.Modifiers).String()
			if structField.NameTag == "" && ddlTag == "" {
				continue
			}
			buf.WriteString(" `")
			written := false
			if structField.NameTag != "" {
				if written {
					buf.WriteString(" ")
				}
				written = true
				buf.WriteString(`sq:` + strconv.Quote(structField.NameTag))
			}
			if ddlTag != "" {
				if written {
					buf.WriteString(" ")
				}
				written = true
				buf.WriteString(`ddl:` + strconv.Quote(ddlTag))
			}
			buf.WriteString("`")
		}
		buf.WriteString("\n}\n")
	}
	b := make([]byte, buf.Len())
	copy(b, buf.Bytes())
	return b, nil
}

func getModelType(dialect string, column *Column) (fieldType string) {
	if column.IsEnum {
		return "sq.EnumField"
	}
	if strings.HasSuffix(column.ColumnType, "[]") {
		return "sq.ArrayField"
	}
	normalizedType, arg1, _ := normalizeColumnType(dialect, column.ColumnType)
	if normalizedType == "TINYINT" && arg1 == "1" {
		return "sq.BooleanField"
	}
	if normalizedType == "BINARY" && arg1 == "16" {
		return "sq.UUIDField"
	}
	switch normalizedType {
	case "BYTEA", "BINARY", "VARBINARY", "TINYBLOB", "BLOB", "MEDIUMBLOB", "LONGBLOB", "VARBIT":
		return "sq.BinaryField"
	case "BOOLEAN", "BIT":
		return "sq.BooleanField"
	case "JSON", "JSONB":
		return "sq.JSONField"
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT", "NUMERIC", "FLOAT", "REAL", "DOUBLE PRECISION":
		return "sq.NumberField"
	case "TINYTEXT", "TEXT", "MEDIUMTEXT", "LONGTEXT", "CHAR", "VARCHAR", "NVARCHAR":
		return "sq.StringField"
	case "DATE", "TIME", "TIMETZ", "DATETIME", "DATETIME2", "SMALLDATETIME", "DATETIMEOFFSET", "TIMESTAMP", "TIMESTAMPTZ":
		return "sq.TimeField"
	case "UUID", "UNIQUEIDENTIFIER":
		return "sq.UUIDField"
	}
	return "sq.AnyField"
}
