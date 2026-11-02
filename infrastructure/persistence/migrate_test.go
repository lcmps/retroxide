package persistence

import "testing"

func TestMigrationSetsStayInVersionNameParity(t *testing.T) {
	if len(sqliteMigrations) != len(mysqlMigrations) {
		t.Fatalf("migration counts sqlite=%d mysql=%d", len(sqliteMigrations), len(mysqlMigrations))
	}
	for index := range sqliteMigrations {
		if sqliteMigrations[index].version != mysqlMigrations[index].version || sqliteMigrations[index].name != mysqlMigrations[index].name {
			t.Errorf("migration %d differs: sqlite=%+v mysql=%+v", index, sqliteMigrations[index], mysqlMigrations[index])
		}
	}
}

func TestSplitMigrationStatements(t *testing.T) {
	statements := splitMigrationStatements("CREATE TABLE a (id INT);\n CREATE INDEX ix ON a(id); ;")
	if len(statements) != 2 || statements[0] != "CREATE TABLE a (id INT)" || statements[1] != "CREATE INDEX ix ON a(id)" {
		t.Fatalf("statements = %#v", statements)
	}
}

func TestValidateMigrationHistoryRejectsUnknownVersionOrName(t *testing.T) {
	tests := []struct {
		name    string
		records []SchemaMigrationModel
	}{
		{name: "unknown version", records: []SchemaMigrationModel{{Version: 99, Name: "future"}}},
		{name: "wrong name", records: []SchemaMigrationModel{{Version: 1, Name: "other"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateMigrationHistory(test.records, sqliteMigrations); err == nil {
				t.Fatal("validateMigrationHistory() succeeded, want error")
			}
		})
	}
}

func TestValidateMigrationHistoryAcceptsRustVersionNine(t *testing.T) {
	records := make([]SchemaMigrationModel, 0, len(sqliteMigrations))
	for _, item := range sqliteMigrations {
		records = append(records, SchemaMigrationModel{Version: item.version, Name: item.name})
	}
	if err := validateMigrationHistory(records, sqliteMigrations); err != nil {
		t.Fatal(err)
	}
}
