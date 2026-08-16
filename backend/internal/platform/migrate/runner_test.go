package migrate

import (
	"reflect"
	"testing"
)

func TestSplitSQLStatements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "simple statements",
			sql:  "CREATE TABLE demo (id integer); CREATE INDEX demo_idx ON demo (id);",
			want: []string{
				"CREATE TABLE demo (id integer)",
				"CREATE INDEX demo_idx ON demo (id)",
			},
		},
		{
			name: "single quoted literal with semicolon",
			sql:  "INSERT INTO demo (message) VALUES ('hello; world'); SELECT 1;",
			want: []string{
				"INSERT INTO demo (message) VALUES ('hello; world')",
				"SELECT 1",
			},
		},
		{
			name: "dollar quoted do block",
			sql:  "CREATE TABLE demo (id integer);\nDO $$\nBEGIN\n    IF EXISTS (SELECT 1) THEN\n        PERFORM 1;\n    END IF;\nEND $$;\nCREATE INDEX demo_idx ON demo (id);",
			want: []string{
				"CREATE TABLE demo (id integer)",
				"DO $$\nBEGIN\n    IF EXISTS (SELECT 1) THEN\n        PERFORM 1;\n    END IF;\nEND $$",
				"CREATE INDEX demo_idx ON demo (id)",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := splitSQLStatements(test.sql)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected split statements %q, got %q", test.want, got)
			}
		})
	}
}
