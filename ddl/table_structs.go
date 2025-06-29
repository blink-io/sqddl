package ddl

import (
	"bytes"
	"database/sql"
	"fmt"
	"github.com/huandu/xstrings"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

const (
	primaryKeyName = "primarykey"
	foreignkeyName = "foreignkey"
	uniqueName     = "unique"
)

// TableStructs is a slice of TableStructs.
type TableStructs struct {
	Tables []TableStruct

	HasTimeType bool
}

// TableStruct represents a table struct.
type TableStruct struct {
	// Name is the name of the table struct.
	Name string

	// Fields are the table struct fields.
	Fields []StructField

	// PKFields are the table struct fields for primary keys.
	PKFields []StructField
}

// StructField represents a struct field within a table struct.
type StructField struct {
	// Name is the name of the struct field.
	Name string

	// Type is the type of the struct field.
	Type string

	// GoType is the Go type of the struct field.
	GoType string

	// NameTag is the value for the "sq" struct tag.
	NameTag string

	// Modifiers are the parsed modifiers for the "ddl" struct tag.
	Modifiers []Modifier

	// tagPos tracks where in the source code the struct tag appeared in. Used
	// for error reporting.
	tagPos token.Pos
}

// NewTableStructs introspects a database connection and returns a slice of
// TableStructs, each TableStruct corresponding to a table in the database. You
// may narrow down the list of tables by filling in the Schemas,
// ExcludeSchemas, Tables and ExcludeTables fields of the Filter struct. The
// Filter.ObjectTypes field will always be set to []string{"TABLES"}.
func NewTableStructs(dialect string, db *sql.DB, filter Filter) (*TableStructs, error) {
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
	tableStructs := new(TableStructs)
	err = tableStructs.ReadCatalog(&catalog)
	if err != nil {
		return nil, err
	}
	return tableStructs, nil
}

// ReadCatalog reads from a catalog and populates the TableStructs accordingly.
func (s *TableStructs) ReadCatalog(catalog *Catalog) error {
	buf := bufpool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufpool.Put(buf)
	for _, schema := range catalog.Schemas {
		for _, table := range schema.Tables {
			tableStruct := TableStruct{
				Name:   strings.ToUpper(strings.ReplaceAll(table.TableName, " ", "_")),
				Fields: make([]StructField, 0, len(table.Columns)+1),
			}
			// sq.TableStruct `ddl:"primarykey=iid,sid"`
			// sq.TableStruct
			// sq.TableStruct `sq:"public.new words"`
			tableStruct.Fields = append(tableStruct.Fields, StructField{Type: "sq.TableStruct"})
			firstField := &tableStruct.Fields[0]
			if (table.TableSchema != "" && table.TableSchema != catalog.CurrentSchema) || needsQuoting(table.TableName) {
				if table.TableSchema != "" {
					firstField.NameTag = table.TableSchema + "." + table.TableName
				} else {
					firstField.NameTag = table.TableName
				}
			}
			if catalog.Dialect == DialectSQLite && isVirtualTable(&table) {
				firstField.Modifiers = append(firstField.Modifiers, Modifier{Name: "virtual"})
			}
			constraintModifierList := make([]*Modifier, 0, len(table.Constraints))
			indexModifierList := make([]*Modifier, 0, len(table.Indexes))
			var primaryKeyModifier *Modifier
			uniqueModifiers := make(map[string]*Modifier)
			foreignkeyModifiers := make(map[string]*Modifier)
			indexModifiers := make(map[string]*Modifier)
			addedModifier := make(map[*Modifier]bool)
			var primaryKeyColumns []string
			for _, constraint := range table.Constraints {
				if constraint.Ignore {
					continue
				}
				columnNames := strings.Join(constraint.Columns, ",")
				m := &Modifier{Value: columnNames}
				switch constraint.ConstraintType {
				case PRIMARY_KEY:
					m.Name = primaryKeyName
					primaryKeyModifier = m
					primaryKeyColumns = append(primaryKeyColumns, constraint.Columns...)
				case UNIQUE:
					m.Name = uniqueName
					uniqueModifiers[columnNames] = m
				case FOREIGN_KEY:
					m.Name = foreignkeyName
					foreignkeyModifiers[columnNames] = m
					buf.Reset()
					if constraint.ReferencesSchema != "" && constraint.ReferencesSchema != catalog.CurrentSchema {
						buf.WriteString(constraint.ReferencesSchema + ".")
					}
					buf.WriteString(constraint.ReferencesTable)
					columnsEqual := true
					for i := range constraint.Columns {
						if i >= len(constraint.ReferencesColumns) {
							columnsEqual = false
							break
						}
						if constraint.Columns[i] != constraint.ReferencesColumns[i] {
							columnsEqual = false
							break
						}
					}
					if !columnsEqual {
						buf.WriteString("." + strings.Join(constraint.ReferencesColumns, ","))
					}
					// references
					m.Submodifiers = append(m.Submodifiers, Modifier{Name: "references", RawValue: buf.String()})
					// onupdate
					if constraint.UpdateRule != "" && constraint.UpdateRule != NO_ACTION {
						m.Submodifiers = append(m.Submodifiers, Modifier{
							Name:  "onupdate",
							Value: strings.ToLower(strings.ReplaceAll(constraint.UpdateRule, " ", "")),
						})
					}
					// ondelete
					if constraint.DeleteRule != "" && constraint.DeleteRule != NO_ACTION {
						m.Submodifiers = append(m.Submodifiers, Modifier{
							Name:  "ondelete",
							Value: strings.ToLower(strings.ReplaceAll(constraint.DeleteRule, " ", "")),
						})
					}
				default:
					continue
				}
				// deferred deferrable
				if constraint.IsDeferrable {
					if constraint.IsInitiallyDeferred {
						m.Submodifiers = append(m.Submodifiers, Modifier{Name: "deferred"})
					} else {
						m.Submodifiers = append(m.Submodifiers, Modifier{Name: "deferrable"})
					}
				}
				constraintModifierList = append(constraintModifierList, m)
			}
			for _, index := range table.Indexes {
				if index.Ignore || !isSimpleIndex(index) {
					continue
				}
				columnNames := strings.Join(index.Columns, ",")
				m := &Modifier{Name: "index", Value: columnNames}
				indexModifiers[columnNames] = m
				// unique
				if index.IsUnique {
					m.Submodifiers = append(m.Submodifiers, Modifier{Name: "unique"})
				}
				// using
				if index.IndexType != "" && !strings.EqualFold(index.IndexType, "BTREE") {
					m.Submodifiers = append(m.Submodifiers, Modifier{Name: "using", RawValue: index.IndexType})
				}
				// foreignkey.index
				if foreignkeyModifier := foreignkeyModifiers[columnNames]; foreignkeyModifier != nil {
					addedModifier[m] = true
					foreignkeyModifier.Submodifiers = append(foreignkeyModifier.Submodifiers, *m)
					foreignkeyModifier.Submodifiers[len(foreignkeyModifier.Submodifiers)-1].Value = ""
				}
				indexModifierList = append(indexModifierList, m)
			}
			for i, column := range table.Columns {
				if column.Ignore {
					continue
				}
				structField := StructField{
					Name:   strings.ToUpper(strings.ReplaceAll(column.ColumnName, " ", "_")),
					Type:   getFieldType(catalog.Dialect, &table.Columns[i]),
					GoType: getFieldGoType(catalog.Dialect, &table.Columns[i]),
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
				// type
				if column.DomainName != "" {
					structField.Modifiers = append(structField.Modifiers, Modifier{Name: "type", RawValue: column.DomainName})
				} else if column.ColumnType != "" && column.ColumnType != defaultColumnType {
					isSQLiteRowid := catalog.Dialect == DialectSQLite &&
						primaryKeyModifier != nil &&
						primaryKeyModifier.Value == column.ColumnName &&
						strings.EqualFold(column.ColumnType, "INTEGER")
					if !isSQLiteRowid {
						structField.Modifiers = append(structField.Modifiers, Modifier{Name: "type", RawValue: column.ColumnType})
					}
				}
				// notnull
				if column.IsNotNull {
					structField.Modifiers = append(structField.Modifiers, Modifier{Name: "notnull"})
				}
				// primarykey
				if primaryKeyModifier != nil && primaryKeyModifier.Value == column.ColumnName {
					addedModifier[primaryKeyModifier] = true
					structField.Modifiers = append(structField.Modifiers, *primaryKeyModifier)
					structField.Modifiers[len(structField.Modifiers)-1].Value = ""
				}
				// unique
				if uniqueModifier := uniqueModifiers[column.ColumnName]; uniqueModifier != nil {
					addedModifier[uniqueModifier] = true
					structField.Modifiers = append(structField.Modifiers, *uniqueModifier)
					structField.Modifiers[len(structField.Modifiers)-1].Value = ""
				}
				// references
				if foreignkeyModifier := foreignkeyModifiers[column.ColumnName]; foreignkeyModifier != nil {
					addedModifier[foreignkeyModifier] = true
					structField.Modifiers = append(structField.Modifiers, *foreignkeyModifier)
					i := len(structField.Modifiers) - 1
					structField.Modifiers[i].Name = "references"
					structField.Modifiers[i].Value = structField.Modifiers[i].Submodifiers[0].RawValue
					structField.Modifiers[i].Submodifiers = structField.Modifiers[i].Submodifiers[1:]
				}
				// autoincrement
				if column.IsAutoincrement {
					switch catalog.Dialect {
					case DialectSQLite:
						structField.Modifiers = append(structField.Modifiers, Modifier{Name: "autoincrement"})
					case DialectMySQL:
						structField.Modifiers = append(structField.Modifiers, Modifier{Name: "auto_increment"})
					}
				}
				// identity
				if column.ColumnIdentity != "" {
					switch catalog.Dialect {
					case DialectPostgres:
						if column.ColumnIdentity == DEFAULT_IDENTITY {
							structField.Modifiers = append(structField.Modifiers, Modifier{Name: "identity"})
						} else if column.ColumnIdentity == ALWAYS_IDENTITY {
							structField.Modifiers = append(structField.Modifiers, Modifier{Name: "alwaysidentity"})
						}
					case DialectSQLServer:
						if column.ColumnIdentity == IDENTITY {
							structField.Modifiers = append(structField.Modifiers, Modifier{Name: "identity"})
						}
					}
				}
				// default
				if column.ColumnDefault != "" && !strings.ContainsRune(column.ColumnDefault, '`') {
					structField.Modifiers = append(structField.Modifiers, Modifier{Name: "default", RawValue: unwrapBrackets(column.ColumnDefault)})
				}
				// onupdatecurrenttimestamp
				if column.OnUpdateCurrentTimestamp {
					structField.Modifiers = append(structField.Modifiers, Modifier{Name: "onupdatecurrenttimestamp"})
				}
				// collate
				if column.CollationName != "" && column.CollationName != catalog.DefaultCollation {
					structField.Modifiers = append(structField.Modifiers, Modifier{Name: "collate", RawValue: column.CollationName})
				}
				// index
				if indexModifier := indexModifiers[column.ColumnName]; indexModifier != nil {
					if !addedModifier[indexModifier] {
						addedModifier[indexModifier] = true
						structField.Modifiers = append(structField.Modifiers, *indexModifier)
						structField.Modifiers[len(structField.Modifiers)-1].Value = ""
					}
				}
				// generated
				if column.IsGenerated || column.GeneratedExpr != "" {
					structField.Modifiers = append(structField.Modifiers, Modifier{Name: "generated"})
				}
				// Add fields for primary keys
				if slices.Contains(primaryKeyColumns, column.ColumnName) {
					//
					tableStruct.PKFields = append(tableStruct.PKFields, structField)

					if s.HasTimeType == false && structField.GoType == "time.Time" {
						s.HasTimeType = true
					}
				}
				tableStruct.Fields = append(tableStruct.Fields, structField)
			}

			if primaryKeyModifier != nil && !addedModifier[primaryKeyModifier] {
				addedModifier[primaryKeyModifier] = true
				tableStruct.Fields[0].Modifiers = Modifiers{*primaryKeyModifier}
			}
			for _, constraintModifier := range constraintModifierList {
				if addedModifier[constraintModifier] {
					continue
				}
				tableStruct.Fields = append(tableStruct.Fields, StructField{
					Name:      "_",
					Type:      "struct{}",
					Modifiers: Modifiers{*constraintModifier},
				})
			}
			for _, indexModifier := range indexModifierList {
				if addedModifier[indexModifier] {
					continue
				}
				tableStruct.Fields = append(tableStruct.Fields, StructField{
					Name:      "_",
					Type:      "struct{}",
					Modifiers: Modifiers{*indexModifier},
				})
			}

			s.Tables = append(s.Tables, tableStruct)
		}
	}
	return nil
}

// MarshalText converts the TableStructs into Go source code.
func (s *TableStructs) MarshalText() (text []byte, err error) {
	buf := bufpool.Get().(*bytes.Buffer)
	enumBuf := bufpool.Get().(*bytes.Buffer)
	buf.Reset()
	enumBuf.Reset()
	defer func() {
		bufpool.Put(buf)
		bufpool.Put(enumBuf)
	}()
	if s.HasTimeType {
		buf.WriteString("import \"time\"\n\n")
	}

	//type tables struct {
	//	UserDevices USER_DEVICES
	//}
	//
	//var Tables = tables{
	//	UserDevices: sq.New[USER_DEVICES](""),
	//}
	buf.WriteString("type tables struct {")
	for _, tableStruct := range s.Tables {
		buf.WriteString(fmt.Sprintf("\n\t%s %s",
			xstrings.ToPascalCase(tableStruct.Name),
			tableStruct.Name),
		)
	}
	buf.WriteString("\n}\n\n")

	buf.WriteString("var Tables = tables {")
	for _, tableStruct := range s.Tables {
		buf.WriteString(fmt.Sprintf("\n\t%s: sq.New[%s](\"\"),",
			xstrings.ToPascalCase(tableStruct.Name),
			tableStruct.Name),
		)
	}
	buf.WriteString("\n}\n")

	// Enum
	//type ENUM_FILM_RATING string
	//
	//func (e ENUM_FILM_RATING) Enumerate() []string {
	//	//TODO Add more
	//	return []string{}
	//}

	for _, tableStruct := range s.Tables {
		hasColumn := false
		for i := len(tableStruct.Fields) - 1; i >= 0; i-- {
			if tableStruct.Fields[i].Name != "" && tableStruct.Fields[i].Name != "_" {
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
		buf.WriteString("type " + tableStruct.Name + " struct {")
		for _, structField := range tableStruct.Fields {
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

			if structField.Type == "sq.EnumField" {
				//Enum type naming rule: Enum{tableStruct.Name}{structField.Name}
				//
				//type EnumEnumsStatus string
				//
				//const EnumEnumsStatusUnknown EnumEnumsStatus = "unknown"
				//
				//var enumEnumsStatusValues = []string{
				//	string(EnumEnumsStatusUnknown),
				//}
				//
				//func (e EnumEnumsStatus) Enumerate() []string {
				//	return enumEnumsStatusValues
				//}
				enumTypeName := normalizeEnumName(tableStruct.Name, structField.Name)
				enumTypeNameValues := enumTypeName + "Values"
				enumTypeDefStmt := "type " + enumTypeName + " string"
				enumBuf.WriteString("\n" + enumTypeDefStmt)
				enumBuf.WriteString(fmt.Sprintf("\n\nconst %sUnknown %s = \"unknown\"", enumTypeName, enumTypeName))
				enumBuf.WriteString(fmt.Sprintf("\n\nvar %s = []string{", enumTypeNameValues))
				enumBuf.WriteString(fmt.Sprintf("\nstring(%sUnknown),", enumTypeName))
				enumBuf.WriteString(fmt.Sprintf("\n}\n\n"))
				enumBuf.WriteString(fmt.Sprintf("\n\nfunc (e %s) Enumerate() []string {", enumTypeName))
				enumBuf.WriteString("\n\treturn " + enumTypeNameValues)
				enumBuf.WriteString("\n}\n\n")
			}
		}
		buf.WriteString("\n}\n\n")

		if len(tableStruct.PKFields) > 0 {
			// PrimaryKeys() example:
			//func (t Table) PrimaryKeys() sq.RowValue {
			//	return sq.RowValue{t.IID, t.SID}
			//}
			buf.WriteString(`func (t ` + tableStruct.Name + `) PrimaryKeys() sq.RowValue {`)
			buf.WriteString("\n\treturn sq.RowValue{")
			for idx, pkField := range tableStruct.PKFields {
				if idx > 0 {
					buf.WriteString(",t." + pkField.Name)
				} else {
					buf.WriteString("t." + pkField.Name)
				}
			}
			buf.WriteString("}")
			buf.WriteString("\n}\n\n")

			//
			//func (s Table) PrimaryKeyValues(id1, id2 int64) sq.Predicate {
			//	return s.PrimaryKeys().In(sq.RowValues{{id1, id2}})
			//}
			buf.WriteString(`func (t ` + tableStruct.Name + `) PrimaryKeyValues(`)
			for idx, pkField := range tableStruct.PKFields {
				if idx > 0 {
					buf.WriteString(", " + normalizeFieldName(pkField.Name) + " " + pkField.GoType)
				} else {
					buf.WriteString(normalizeFieldName(pkField.Name) + " " + pkField.GoType)
				}
			}
			buf.WriteString(`) sq.Predicate {`)
			buf.WriteString("\n\treturn t.PrimaryKeys().In(sq.RowValues{{")
			for idx, pkField := range tableStruct.PKFields {
				if idx > 0 {
					buf.WriteString(", " + normalizeFieldName(pkField.Name))
				} else {
					buf.WriteString(normalizeFieldName(pkField.Name))
				}
			}
			buf.WriteString("}})")
			buf.WriteString("\n}")

			if enumBuf.Len() > 0 {
				buf.WriteString(enumBuf.String())
				enumBuf.Reset()
			}
		}

		buf.WriteString("\n")
	} //foe end

	b := make([]byte, buf.Len())
	copy(b, buf.Bytes())
	return b, nil
}

func getFieldType(dialect string, column *Column) (fieldType string) {
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

func getFieldGoType(dialect string, column *Column) (fieldType string) {
	if column.IsEnum {
		return "Enum"
	}
	if strings.HasSuffix(column.ColumnType, "[]") {
		idx := strings.Index(column.ColumnType, "[")
		colType := column.ColumnType[:idx]
		var cc = new(Column)
		cc.ColumnType = colType
		return "[]" + getFieldGoType(dialect, cc)
	}
	normalizedType, arg1, _ := normalizeColumnType(dialect, column.ColumnType)
	if normalizedType == "TINYINT" && arg1 == "1" {
		return "bool"
	}
	if normalizedType == "BINARY" && arg1 == "16" {
		return "[16]byte"
	}
	switch normalizedType {
	case "BYTEA", "BINARY", "VARBINARY", "TINYBLOB", "BLOB", "MEDIUMBLOB", "LONGBLOB", "VARBIT":
		return "[]byte"
	case "BOOLEAN", "BIT":
		return "bool"
	case "JSON", "JSONB":
		return "map[string]any"
	case "TINYINT":
		return "int8"
	case "SMALLINT":
		return "int16"
	case "MEDIUMINT": // 24 bytes in MySQL
		return "int32"
	case "INT", "INTEGER":
		if DialectSQLite == dialect {
			return "int64"
		} else {
			return "int32"
		}
	case "BIGINT":
		return "int64"
	case "NUMERIC", "FLOAT":
		return "float32"
	case "REAL", "DOUBLE PRECISION":
		return "float64"
	case "TINYTEXT", "TEXT", "MEDIUMTEXT", "LONGTEXT", "CHAR", "VARCHAR", "NVARCHAR":
		return "string"
	case "DATE", "TIME", "TIMETZ", "DATETIME", "DATETIME2", "SMALLDATETIME", "DATETIMEOFFSET", "TIMESTAMP", "TIMESTAMPTZ":
		return "time.Time"
	case "UUID", "UNIQUEIDENTIFIER":
		return "[16]byte"
	default:
		return "any"
	}
}

func needsQuoting(identifier string) bool {
	for i, char := range identifier {
		if i == 0 && (char >= '0' && char <= '9') {
			return true
		}
		if char == '_' || (char >= '0' && char <= '9') || (char >= 'a' && char <= 'z') {
			continue
		}
		return true
	}
	return false
}

// We only consider simple indexes for table structs because complex indexes
// involving predicates or included columns are harder to diff.
func isSimpleIndex(index Index) bool {
	if len(index.Columns) == 0 {
		return false
	}
	if len(index.IncludeColumns) > 0 {
		return false
	}
	if index.Predicate != "" {
		return false
	}
	for _, isDescending := range index.Descending {
		if isDescending {
			return false
		}
	}
	for _, opclass := range index.Opclasses {
		if strings.Count(opclass, "_") > 1 {
			return false
		}
	}
	for _, column := range index.Columns {
		if strings.HasPrefix(column, "(") {
			return false
		}
	}
	upperSQL := strings.ToUpper(index.SQL)
	if strings.Contains(upperSQL, " WHERE ") {
		return false
	}
	if strings.Contains(upperSQL, " DESC") {
		return false
	}
	if strings.Contains(upperSQL, " INCLUDE ") {
		return false
	}
	return true
}
