package migrate

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

const publicSchema = "public"

type columnSpec struct {
	Name        string
	DataType    string
	DefaultExpr string
	NotNull     bool
	SerialType  string
}

type constraintSpec struct {
	Type       string
	Name       string
	Definition string
}

type tableSpec struct {
	Name        string
	IsUnlogged  bool
	Columns     []columnSpec
	Constraints []constraintSpec
	Indexes     []string
}

type snapshotOptions struct {
	ExcludedTables map[string]struct{}
}

func SnapshotApplicationSchema(ctx context.Context, conn *pgx.Conn) (string, error) {
	return snapshotPublicSchema(ctx, conn, snapshotOptions{
		ExcludedTables: map[string]struct{}{
			HistoryTable: {},
		},
	})
}

func snapshotPublicSchema(ctx context.Context, conn *pgx.Conn, options snapshotOptions) (string, error) {
	tables, err := loadTableSpecs(ctx, conn, options)
	if err != nil {
		return "", err
	}

	var rendered bytes.Buffer
	for index, table := range tables {
		if index > 0 {
			rendered.WriteString("\n")
		}

		renderTable(&rendered, table)
	}

	for _, table := range tables {
		for _, constraint := range table.Constraints {
			if constraint.Type == "f" {
				continue
			}
			fmt.Fprintf(
				&rendered,
				"ALTER TABLE ONLY %s ADD CONSTRAINT %s %s;\n",
				qualifyTable(table.Name),
				quoteIdent(constraint.Name),
				constraint.Definition,
			)
		}
	}

	for _, table := range tables {
		for _, constraint := range table.Constraints {
			if constraint.Type != "f" {
				continue
			}
			fmt.Fprintf(
				&rendered,
				"ALTER TABLE ONLY %s ADD CONSTRAINT %s %s;\n",
				qualifyTable(table.Name),
				quoteIdent(constraint.Name),
				constraint.Definition,
			)
		}
	}

	if len(tables) > 0 {
		rendered.WriteString("\n")
	}

	for _, table := range tables {
		for _, indexDefinition := range table.Indexes {
			rendered.WriteString(indexDefinition)
			rendered.WriteString(";\n")
		}
	}

	return NormalizeSchemaSQL(rendered.String()), nil
}

func NormalizeSchemaSQL(value string) string {
	lines := strings.Split(value, "\n")
	normalized := make([]string, 0, len(lines))
	previousBlank := true

	for _, line := range lines {
		trimmedRight := strings.TrimRight(line, " \t")
		trimmedLeft := strings.TrimSpace(trimmedRight)
		if strings.HasPrefix(trimmedLeft, "--") {
			continue
		}
		isBlank := trimmedLeft == ""
		if isBlank {
			if previousBlank {
				continue
			}
			previousBlank = true
			normalized = append(normalized, "")
			continue
		}

		previousBlank = false
		normalized = append(normalized, trimmedRight)
	}

	return strings.TrimSpace(strings.Join(normalized, "\n"))
}

func loadTableSpecs(ctx context.Context, conn *pgx.Conn, options snapshotOptions) ([]tableSpec, error) {
	tables, err := loadTables(ctx, conn, options)
	if err != nil {
		return nil, err
	}

	if len(tables) == 0 {
		return nil, nil
	}

	if err := loadColumns(ctx, conn, tables); err != nil {
		return nil, err
	}
	if err := loadConstraints(ctx, conn, tables); err != nil {
		return nil, err
	}
	if err := loadIndexes(ctx, conn, tables); err != nil {
		return nil, err
	}

	specs := make([]tableSpec, 0, len(tables))
	for _, name := range sortedTableNames(tables) {
		specs = append(specs, *tables[name])
	}

	return specs, nil
}

func loadTables(ctx context.Context, conn *pgx.Conn, options snapshotOptions) (map[string]*tableSpec, error) {
	rows, err := conn.Query(
		ctx,
		`SELECT c.relname, c.relpersistence::text
		FROM pg_class AS c
		JOIN pg_namespace AS n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind = 'r'
		ORDER BY c.relname`,
		publicSchema,
	)
	if err != nil {
		return nil, fmt.Errorf("query public tables: %w", err)
	}
	defer rows.Close()

	tables := map[string]*tableSpec{}
	for rows.Next() {
		var (
			name         string
			relpersisted string
		)
		if err := rows.Scan(&name, &relpersisted); err != nil {
			return nil, fmt.Errorf("scan public table: %w", err)
		}

		if _, excluded := options.ExcludedTables[name]; excluded {
			continue
		}

		tables[name] = &tableSpec{
			Name:       name,
			IsUnlogged: relpersisted == "u",
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public tables: %w", err)
	}

	return tables, nil
}

func loadColumns(ctx context.Context, conn *pgx.Conn, tables map[string]*tableSpec) error {
	rows, err := conn.Query(
		ctx,
		`SELECT
			c.relname,
			a.attname,
			pg_catalog.format_type(a.atttypid, a.atttypmod) AS data_type,
			a.attnotnull,
			pg_get_expr(ad.adbin, ad.adrelid) AS default_expr,
			pg_get_serial_sequence(format('%I.%I', n.nspname, c.relname), a.attname) AS serial_sequence
		FROM pg_class AS c
		JOIN pg_namespace AS n ON n.oid = c.relnamespace
		JOIN pg_attribute AS a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		LEFT JOIN pg_attrdef AS ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relkind = 'r'
		ORDER BY c.relname, a.attnum`,
		publicSchema,
	)
	if err != nil {
		return fmt.Errorf("query public columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			tableName      string
			columnName     string
			dataType       string
			notNull        bool
			defaultExpr    *string
			serialSequence *string
		)
		if err := rows.Scan(
			&tableName,
			&columnName,
			&dataType,
			&notNull,
			&defaultExpr,
			&serialSequence,
		); err != nil {
			return fmt.Errorf("scan public column: %w", err)
		}

		table := tables[tableName]
		if table == nil {
			continue
		}

		column := columnSpec{
			Name:     columnName,
			DataType: dataType,
			NotNull:  notNull,
		}
		if defaultExpr != nil {
			column.DefaultExpr = *defaultExpr
		}
		if serialSequence != nil {
			column.SerialType = classifySerialType(dataType, column.DefaultExpr, *serialSequence)
		}

		table.Columns = append(table.Columns, column)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate public columns: %w", err)
	}

	return nil
}

func loadConstraints(ctx context.Context, conn *pgx.Conn, tables map[string]*tableSpec) error {
	rows, err := conn.Query(
		ctx,
		`SELECT
			c.relname,
			con.contype::text,
			con.conname,
			pg_get_constraintdef(con.oid, true) AS definition
		FROM pg_constraint AS con
		JOIN pg_class AS c ON c.oid = con.conrelid
		JOIN pg_namespace AS n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind = 'r' AND con.contype IN ('p', 'u', 'f', 'c')
		ORDER BY c.relname, con.contype, con.conname`,
		publicSchema,
	)
	if err != nil {
		return fmt.Errorf("query public constraints: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			tableName      string
			constraintType string
			name           string
			definition     string
		)
		if err := rows.Scan(&tableName, &constraintType, &name, &definition); err != nil {
			return fmt.Errorf("scan public constraint: %w", err)
		}

		table := tables[tableName]
		if table == nil {
			continue
		}

		table.Constraints = append(table.Constraints, constraintSpec{
			Type:       constraintType,
			Name:       name,
			Definition: normalizeConstraintDefinition(definition),
		})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate public constraints: %w", err)
	}

	return nil
}

func loadIndexes(ctx context.Context, conn *pgx.Conn, tables map[string]*tableSpec) error {
	rows, err := conn.Query(
		ctx,
		`SELECT
			t.relname,
			pg_get_indexdef(i.oid, 0, true) AS definition
		FROM pg_class AS t
		JOIN pg_namespace AS n ON n.oid = t.relnamespace
		JOIN pg_index AS idx ON idx.indrelid = t.oid
		JOIN pg_class AS i ON i.oid = idx.indexrelid
		WHERE n.nspname = $1
		  AND t.relkind = 'r'
		  AND NOT EXISTS (
			SELECT 1
			FROM pg_constraint AS con
			WHERE con.conindid = i.oid
		  )
		ORDER BY t.relname, i.relname`,
		publicSchema,
	)
	if err != nil {
		return fmt.Errorf("query public indexes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			tableName  string
			definition string
		)
		if err := rows.Scan(&tableName, &definition); err != nil {
			return fmt.Errorf("scan public index: %w", err)
		}

		table := tables[tableName]
		if table == nil {
			continue
		}

		table.Indexes = append(table.Indexes, definition)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate public indexes: %w", err)
	}

	return nil
}

func sortedTableNames(tables map[string]*tableSpec) []string {
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func renderTable(buffer *bytes.Buffer, table tableSpec) {
	tableKind := "CREATE TABLE"
	if table.IsUnlogged {
		tableKind = "CREATE UNLOGGED TABLE"
	}

	fmt.Fprintf(buffer, "%s %s (\n", tableKind, qualifyTable(table.Name))
	for index, column := range table.Columns {
		fmt.Fprintf(buffer, "    %s %s", quoteIdent(column.Name), renderColumnType(column))
		if column.DefaultExpr != "" && column.SerialType == "" {
			fmt.Fprintf(buffer, " DEFAULT %s", column.DefaultExpr)
		}
		if column.NotNull {
			buffer.WriteString(" NOT NULL")
		}
		if index < len(table.Columns)-1 {
			buffer.WriteString(",")
		}
		buffer.WriteString("\n")
	}
	buffer.WriteString(");\n")
}

func renderColumnType(column columnSpec) string {
	if column.SerialType != "" {
		return column.SerialType
	}

	return column.DataType
}

func classifySerialType(dataType string, defaultExpr string, serialSequence string) string {
	if serialSequence == "" || !strings.HasPrefix(defaultExpr, "nextval(") {
		return ""
	}

	switch dataType {
	case "smallint":
		return "SMALLSERIAL"
	case "integer":
		return "SERIAL"
	case "bigint":
		return "BIGSERIAL"
	default:
		return ""
	}
}

func normalizeConstraintDefinition(definition string) string {
	replacer := strings.NewReplacer(
		"::character varying::text", "::character varying",
		"]::text[]", "]",
	)

	return replacer.Replace(definition)
}

func quoteIdent(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func qualifyTable(name string) string {
	return quoteIdent(publicSchema) + "." + quoteIdent(name)
}
