package ddl

import (
	"bytes"
	"database/sql"
	"fmt"
	"go/token"
	"regexp"
	"slices"
	"strings"

	"github.com/go-openapi/inflect"
	"github.com/huandu/xstrings"
)

// ModelStructs is a slice of ModelStruct.
type ModelStructs struct {
	Models []ModelStruct

	HasTimeType bool

	HasNullField bool
}

// ModelStruct represents a model struct.
type ModelStruct struct {
	// Name is the name of the table struct.
	Name string

	// Fields are the model struct fields.
	Fields []StructField

	// PKFields are the table struct fields for primary keys.
	PKFields []StructField
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
func NewModelStructs(dialect string, db *sql.DB, filter Filter) (*ModelStructs, error) {
	var catalog Catalog
	dbi := &DatabaseIntrospector{
		Filter:  filter,
		Dialect: dialect,
		DB:      db,
	}
	dbi.ObjectTypes = []string{"TABLES"}
	modelStructs := new(ModelStructs)
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

// ReadCatalog reads from a catalog and populates the TableStructs accordingly.
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
			constraintModifierList := make([]*Modifier, 0, len(table.Constraints))
			indexModifierList := make([]*Modifier, 0, len(table.Indexes))
			var primarykeyModifier *Modifier
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
					m.Name = "primarykey"
					primarykeyModifier = m
					primaryKeyColumns = append(primaryKeyColumns, constraint.Columns...)
				case UNIQUE:
					m.Name = "unique"
					uniqueModifiers[columnNames] = m
				case FOREIGN_KEY:
					m.Name = "foreignkey"
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
				if structField.Type == "sq.EnumField" {
					enumTypeName := normalizeEnumName(modelStruct.Name, structField.Name)
					structField.GoType = enumTypeName
				}
				if s.HasTimeType == false && structField.GoType == "time.Time" {
					s.HasTimeType = true
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
						primarykeyModifier != nil &&
						primarykeyModifier.Value == column.ColumnName &&
						strings.EqualFold(column.ColumnType, "INTEGER")
					if !isSQLiteRowid {
						structField.Modifiers = append(structField.Modifiers, Modifier{Name: "type", RawValue: column.ColumnType})
					}
				}
				// notnull
				if column.IsNotNull {
					structField.Modifiers = append(structField.Modifiers, Modifier{Name: "notnull"})
				} else {
					if !s.HasNullField {
						s.HasNullField = true
					}
				}
				// primarykey
				if primarykeyModifier != nil && primarykeyModifier.Value == column.ColumnName {
					addedModifier[primarykeyModifier] = true
					structField.Modifiers = append(structField.Modifiers, *primarykeyModifier)
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
					modelStruct.PKFields = append(modelStruct.PKFields, structField)

					if s.HasTimeType == false && structField.GoType == "time.Time" {
						s.HasTimeType = true
					}
				}
				modelStruct.Fields = append(modelStruct.Fields, structField)
			}
			if primarykeyModifier != nil && !addedModifier[primarykeyModifier] {
				addedModifier[primarykeyModifier] = true
				//modelStruct.Fields[0].Modifiers = Modifiers{*primarykeyModifier}
			}
			for _, constraintModifier := range constraintModifierList {
				if addedModifier[constraintModifier] {
					continue
				}
				modelStruct.Fields = append(modelStruct.Fields, StructField{
					Name:      "_",
					Type:      "struct{}",
					Modifiers: Modifiers{*constraintModifier},
				})
			}
			for _, indexModifier := range indexModifierList {
				if addedModifier[indexModifier] {
					continue
				}
				modelStruct.Fields = append(modelStruct.Fields, StructField{
					Name:      "_",
					Type:      "struct{}",
					Modifiers: Modifiers{*indexModifier},
				})
			}
			s.Models = append(s.Models, modelStruct)
		}
	}
	return nil
}

// MarshalText converts the TableStructs into Go source code.
func (s *ModelStructs) MarshalText() (text []byte, err error) {
	buf := bufpool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufpool.Put(buf)
	buf.WriteString("import \"context\"\n")
	if s.HasTimeType {
		buf.WriteString("import \"time\"\n")
	}
	buf.WriteString("\n")
	if s.HasNullField {
		buf.WriteString("import \"github.com/blink-io/opt/null\"\n")
		buf.WriteString("import \"github.com/blink-io/opt/omitnull\"\n")
	}
	buf.WriteString("import \"github.com/blink-io/sq\"\n")
	buf.WriteString("import \"github.com/blink-io/opt/omit\"\n\n\n")

	for _, modelStruct := range s.Models {
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
		// --- generate model begin ---
		buf.WriteString("type " + normalizeModelName(modelStruct.Name) + " struct {")
		for _, structField := range modelStruct.Fields {
			if structField.Name == "_" {
				continue
			}
			if hasNotNullModifier(structField.Modifiers) {
				buf.WriteString("\n\t" + normalizeFieldName(structField.Name) + " " + structField.GoType)
			} else {
				buf.WriteString("\n\t" + normalizeFieldName(structField.Name) + " null.Val[" + structField.GoType + "]")
			}

			tagVal := "-"
			if slices.ContainsFunc(modelStruct.PKFields, func(v StructField) bool {
				return v.Name != structField.Name
			}) {
				tagVal = normalizeTagName(structField.Name)
			}
			buf.WriteString(fmt.Sprintf("` db:\"%s\" json:\"%s\"`", tagVal, tagVal))
		}
		buf.WriteString("\n}\n\n")
		// --- generate model end ---

		// --- generate model setter begin ---
		buf.WriteString("type " + normalizeModelName(modelStruct.Name) + "Setter struct {")
		for _, structField := range modelStruct.Fields {
			if structField.Name == "_" {
				continue
			}
			if hasNotNullModifier(structField.Modifiers) {
				buf.WriteString("\n\t" + normalizeFieldName(structField.Name) + " omit.Val[" + structField.GoType + "]")
			} else {
				buf.WriteString("\n\t" + normalizeFieldName(structField.Name) + " omitnull.Val[" + structField.GoType + "]")
			}
			tagVal := "-"
			if slices.ContainsFunc(modelStruct.PKFields, func(v StructField) bool {
				return v.Name != structField.Name
			}) {
				tagVal = normalizeTagName(structField.Name)
			}
			buf.WriteString(fmt.Sprintf("` db:\"%s\" json:\"%s\"`", tagVal, tagVal))
		}
		buf.WriteString("\n}\n\n")
		// --- generate model setter end ---

		// Generate column mapper
		//func (t TAGS) ColumnSetter(ctx context.Context, c *sq.Column, s TagSetter) {
		//	s.ID.IfSet(func(v int64) {
		//		c.SetInt64(t.ID, v)
		//	})
		//	s.SID.IfSet(func(v sting) {
		//		c.SetString(t.SID, v)
		//	})
		//  s.EmployeeID.IfSet(func(v [16]byte) {
		//	  c.SetUUID(t.EMPLOYEE_ID, v)
		//  })
		//}
		buf.WriteString(fmt.Sprintf("func (t %s) ColumnMapper(ctx context.Context, c *sq.Column, s %s) {",
			modelStruct.Name,
			normalizeModelName(modelStruct.Name)+"Setter"),
		)
		for _, structField := range modelStruct.Fields {
			if structField.Name == "_" {
				continue
			}
			buf.WriteString(fmt.Sprintf("\n\ts.%s.IfSet(func(v %s) {",
				normalizeFieldName(structField.Name),
				structField.GoType,
			))
			buf.WriteString(fmt.Sprintf("\n\t\tc.%s(t.%s, v)",
				getColumnSetMethod(structField.GoType),
				structField.Name))
			buf.WriteString("\n\t})")
		}
		buf.WriteString("\n}\n\n")

		// Generate row mapper
		//func (t TAGS) RowMapper(r *sq.Row) Tag {
		//	v := Tag{}
		//	v.ID = r.Int64Field(t.ID)
		//	v.GUID = r.StringField(t.GUID)
		//	return v
		//}
		buf.WriteString(fmt.Sprintf("func (t %s) RowMapper(ctx context.Context, r *sq.Row) %s {",
			modelStruct.Name,
			normalizeModelName(modelStruct.Name)),
		)
		buf.WriteString("\n\tv :=" + normalizeModelName(modelStruct.Name) + "{}")
		for _, structField := range modelStruct.Fields {
			if structField.Name == "_" {
				continue
			}
			notNull := hasNotNullModifier(structField.Modifiers)
			modelFieldName := normalizeFieldName(structField.Name)
			rowFieldMethod := getRowFieldMethod(structField.GoType)
			varFieldName := normalizeVarName(structField.Name)

			if notNull {
				switch rowFieldMethod {
				case "UUIDField":
					// Example
					// var uuid [16]byte
					// r.UUIDField(&uuid, t.EMPLOYEE_ID)
					// v.EmployeeID = uuid
					buf.WriteString(fmt.Sprintf("\n\tvar %s [16]byte", varFieldName))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(&%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = %s",
						modelFieldName,
						varFieldName,
					))

				case "JSONField":
					//var data map[string]any
					//r.JSONField(data, t.DATA)
					//v.Data = null.FromPtr(data)
					buf.WriteString(fmt.Sprintf("\n\tvar %s %s", varFieldName, structField.GoType))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = %s",
						modelFieldName,
						varFieldName,
					))

				case "ArrayField":
					//var arrays []any
					//r.ArrayField(arrays, t.DATA)
					//v.Arrays = arrays
					buf.WriteString(fmt.Sprintf("\n\tvar %s %s", varFieldName, structField.GoType))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = %s",
						modelFieldName,
						varFieldName,
					))

				case "EnumField":
					//var enumVal EnumVal
					//r.EnumField(&enumVal, t.ENUM_VAL)
					//v.EnumVal = enumVal
					buf.WriteString(fmt.Sprintf("\n\tvar %s %s", varFieldName, structField.GoType))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = %s",
						modelFieldName,
						varFieldName,
					))

				case "ScanField":
					//var xxType XXType
					//r.ScanField(&enumVal, t.XX_TYPE)
					//v.XXType = xxType
					buf.WriteString(fmt.Sprintf("\n\tvar %s %s", varFieldName, structField.GoType))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = %s",
						modelFieldName,
						varFieldName,
					))

				default:
					// v.CardID = r.Int64Field(t.CAR_ID)
					buf.WriteString(fmt.Sprintf("\n\tv.%s = r.%s(t.%s)",
						modelFieldName,
						rowFieldMethod,
						structField.Name,
					))
				}
			} else {
				switch rowFieldMethod {
				case "BytesField":
					// Example1: BytesField has no NullBytesField
					// cardInfo := r.BytesField(t.CAR_INFO)
					buf.WriteString(fmt.Sprintf("\n\tv.%s = null.Val[[]byte]{}", modelFieldName))
				case "IntField",
					"Int8Field",
					"Int16Field",
					"Int32Field",
					"Int64Field",
					"UintField",
					"Uint8Field",
					"Uint16Field",
					"Uint32Field",
					"Uint64Field",
					"Float32Field",
					"Float64Field",
					"BoolField",
					"StringField",
					"TimeField":
					// carID := r.NullInt32Field(t.CAR_ID)
					// v.CardID = null.FromCond(cardId.V, cardID.Valid)
					newRowFieldMethod := "Null" + rowFieldMethod
					buf.WriteString(fmt.Sprintf("\n\t%s := r.%s(t.%s)",
						varFieldName,
						newRowFieldMethod,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = null.FromCond(%s.V, %s.Valid)",
						modelFieldName,
						varFieldName,
						varFieldName,
					))
				case "UUIDField":
					//var managerID = new([16]byte)
					//r.UUIDField(managerID, t.MANAGER_ID)
					//v.ManagerID = null.FromPtr(managerID)
					buf.WriteString(fmt.Sprintf("\n\tvar %s = new([16]byte)", varFieldName))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = null.FromPtr(%s)",
						modelFieldName,
						varFieldName,
					))

				case "JSONField":
					//data := new(map[string]any)
					//r.JSONField(data, t.DATA)
					//v.Data = null.FromPtr(data)
					buf.WriteString(fmt.Sprintf("\n\tvar %s = new(%s)", varFieldName, structField.GoType))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = null.FromPtr(%s)",
						modelFieldName,
						varFieldName,
					))

				case "ArrayField":
					//var arrays = new([]any)
					//r.ArrayField(arrays, t.DATA)
					//v.Arrays = null.FromPtr(arrays)
					buf.WriteString(fmt.Sprintf("\n\tvar %s = new(%s)", varFieldName, structField.GoType))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = null.FromPtr(%s)",
						modelFieldName,
						varFieldName,
					))

				case "EnumField":
					//var enumVal = new(EnumVal)
					//r.EnumField(enumVal, t.ENUM_VAL)
					//v.EnumVal = null.FromPtr(enumVal)
					buf.WriteString(fmt.Sprintf("\n\tvar %s = new(%s)", varFieldName, structField.GoType))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = null.FromPtr(%s)",
						modelFieldName,
						varFieldName,
					))

				case "ScanField":
					//var xxType = new(XXType)
					//r.ScanField(xxType, t.XX_TYPE)
					//v.XXType = null.FromPtr(xxType)
					buf.WriteString(fmt.Sprintf("\n\tvar %s = new(%s)", varFieldName, structField.GoType))
					buf.WriteString(fmt.Sprintf("\n\tr.%s(%s, t.%s)",
						rowFieldMethod,
						varFieldName,
						structField.Name,
					))
					buf.WriteString(fmt.Sprintf("\n\tv.%s = null.FromPtr(%s)",
						modelFieldName,
						varFieldName,
					))

				default:
				}
			}
		}
		buf.WriteString("\n\treturn v")
		buf.WriteString("\n}\n")
	}
	b := make([]byte, buf.Len())
	copy(b, buf.Bytes())
	return b, nil
}

func hasNotNullModifier(modifiers Modifiers) bool {
	for _, modifier := range modifiers {
		if modifier.Name == "notnull" {
			return true
		}
	}
	return false
}

func getColumnSetMethod(goType string) string {
	switch goType {
	case "int":
		return "SetInt"
	case "int8":
		return "SetInt8"
	case "int16":
		return "SetInt16"
	case "int32":
		return "SetInt32"
	case "int64":
		return "SetInt64"
	case "uint":
		return "SetUint"
	case "uint8":
		return "SetUint8"
	case "uint16":
		return "SetUint16"
	case "uint32":
		return "SetUint32"
	case "uint64":
		return "SetUint64"
	case "string":
		return "SetString"
	case "float32":
		return "SetFloat32"
	case "float64":
		return "SetFloat64"
	case "bool":
		return "SetBool"
	case "time.Time":
		return "SetTime"
	case "[]byte":
		return "SetBytes"
	case "[]int", "[]int8", "[]int16", "[]int32", "[]int64",
		"[]uint", "[]uint8", "[]uint16", "[]uint32", "[]uint64",
		"[]float32", "[]float64", "[]bool", "[]string",
		"[]time.Time", "[][16]byte", "[]map[string]any", "[]any":
		return "SetArray"
	case "[16]byte":
		return "SetUUID"
	case "any":
		return "Set"
	case "map[string]any":
		return "SetJSON"
	default:
		if strings.HasPrefix(goType, "Enum") {
			return "SetEnum"
		} else {
			return "Set"
		}
	}
}

func getRowFieldMethod(goType string) string {
	switch goType {
	case "int":
		return "IntField"
	case "int8":
		return "Int8Field"
	case "int16":
		return "Int16Field"
	case "int32":
		return "Int32Field"
	case "int64":
		return "Int64Field"
	case "uint":
		return "UintField"
	case "uint8":
		return "Uint8Field"
	case "uint16":
		return "Uint16Field"
	case "uint32":
		return "Uint32Field"
	case "uint64":
		return "Uint64Field"
	case "string":
		return "StringField"
	case "float32":
		return "Float32Field"
	case "float64":
		return "Float64Field"
	case "bool":
		return "BoolField"
	case "time.Time":
		return "TimeField"
	case "[]byte":
		return "BytesField"
	case "[16]byte":
		return "UUIDField"
	case "map[string]any":
		return "JSONField"
	case "[]int", "[]int8", "[]int16", "[]int32", "[]int64",
		"[]uint", "[]uint8", "[]uint16", "[]uint32", "[]uint64",
		"[]float32", "[]float64", "[]bool", "[]string",
		"[]time.Time", "[][16]byte", "[]map[string]any", "[]any":
		return "ArrayField"
	case "any":
		return "ScanField"
	default:
		if strings.HasPrefix(goType, "Enum") {
			return "EnumField"
		} else {
			return "ScanField"
		}
	}
}

func normalizePropName(name string, trans func(string) string) string {
	rr, err := regexp.Compile("(_)?([A-Z])*ID$")
	if err == nil {
		if rr.MatchString(name) {
			if i := strings.LastIndex(name, "_"); i > 0 {
				s1 := name[:i]
				s2 := name[i+1:]
				return trans(s1) + s2
			} else {
				return name
			}
		}
	}
	return trans(name)
}

func normalizeModelName(name string) string {
	return inflect.Singularize(normalizePropName(name, xstrings.ToPascalCase))
}

func normalizeFieldName(name string) string {
	return normalizePropName(name, xstrings.ToPascalCase)
}

func normalizeVarName(name string) string {
	return normalizePropName(name, xstrings.ToCamelCase)
}

func normalizeEnumName(modelName, fieldName string) string {
	return "Enum" + normalizeFieldName(modelName) + normalizeFieldName(fieldName)
}

func normalizeTagName(name string) string {
	return xstrings.ToSnakeCase(name)
}
