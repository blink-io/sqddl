package ddl

import (
	"database/sql"
	"go/token"
	"strings"
)

// ModelStructs is a slice of ModelStruct.
type ModelStructs []TableStruct

// ModelStruct represents a table struct.
type ModelStruct struct {
	// Name is the name of the table struct.
	Name string

	// Fields are the table struct fields.
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

	// Modifiers are the parsed modifiers for the "ddl" struct tag.
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

func (s *ModelStructs) ReadCatalog(catalog *Catalog) error {
	return nil
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
