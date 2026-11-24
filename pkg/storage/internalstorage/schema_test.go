package internalstorage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSchemaSQL(t *testing.T) {
	for _, dbType := range []string{"postgres", "mysql", "sqlite"} {
		t.Run(dbType, func(t *testing.T) {
			statements, err := SchemaSQL(dbType)
			if err != nil {
				t.Fatal(err)
			}
			if len(statements) == 0 || !strings.HasPrefix(statements[0], "CREATE TABLE") {
				t.Fatalf("expected the statements to start with CREATE TABLE, got %q", statements)
			}
			if !strings.Contains(strings.Join(statements, "\n"), "uni_group_version_resource_cluster_namespace_name") {
				t.Fatalf("expected the statements to create the unique index, got %q", statements)
			}
		})
	}

	if _, err := SchemaSQL("unknown"); err == nil {
		t.Fatal("expected an error for the unsupported type")
	}
}

func TestPendingSchemaSQL(t *testing.T) {
	db, err := gorm.Open(gsqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB(db)

	exec := func(statements ...string) {
		t.Helper()
		for _, statement := range statements {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatalf("%s: %v", statement, err)
			}
		}
	}
	// apply expects the schema to be out of date until the pending statements are executed.
	apply := func(expected ...string) {
		t.Helper()
		statements, err := pendingSchemaSQL(db)
		if err != nil {
			t.Fatal(err)
		}
		for _, substr := range expected {
			if !strings.Contains(strings.Join(statements, "\n"), substr) {
				t.Fatalf("expected the pending statements to contain %q, got %q", substr, statements)
			}
		}
		if err := checkSchema(db); !errors.Is(err, ErrSchemaOutOfDate) {
			t.Fatalf("expected the schema to be out of date, got %v", err)
		}

		exec(statements...)
		if err := checkSchema(db); err != nil {
			t.Fatalf("expected the schema to be up to date after executing %q, got %v", statements, err)
		}
	}

	apply("CREATE TABLE `resources`")

	exec("ALTER TABLE resources DROP COLUMN owner_uid", "DROP INDEX idx_cluster")
	apply("ADD `owner_uid`", "INDEX `idx_cluster`")

	// sqlite alters a column by rebuilding the table, which drops the indexes
	exec(
		"ALTER TABLE resources DROP COLUMN owner_uid", "ALTER TABLE resources ADD COLUMN owner_uid text",
		"ALTER TABLE resources DROP COLUMN events", "ALTER TABLE resources ADD COLUMN events JSON NOT NULL DEFAULT '{}'",
	)
	apply("CREATE TABLE `resources__temp`", "CREATE UNIQUE INDEX")
}

func TestCheckSchemaUniqueIndex(t *testing.T) {
	db, cleanup, err := newSQLiteDB()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	defer closeDB(db)

	const name = "uni_group_version_resource_cluster_namespace_name"
	for _, test := range []struct {
		index string
		err   error
	}{
		{"INDEX " + name + " ON resources (`group`, version, resource, cluster, namespace, name)", ErrSchemaOutOfDate},
		{"UNIQUE INDEX " + name + " ON resources (`group`, version, resource, cluster, namespace)", ErrSchemaOutOfDate},
		{"UNIQUE INDEX " + name + " ON resources (name, namespace, cluster, resource, version, `group`)", nil},
		{"UNIQUE INDEX extra ON resources (uid)", nil},
	} {
		if strings.Contains(test.index, name) {
			if err := db.Exec("DROP INDEX " + name).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Exec("CREATE " + test.index).Error; err != nil {
			t.Fatal(err)
		}

		if err := checkSchema(db); !errors.Is(err, test.err) {
			t.Errorf("%s: expected %v, got %v", test.index, test.err, err)
		}
	}
}

func TestNewStorageFactoryWithoutAutoMigration(t *testing.T) {
	config := writeTestConfig(t, fmt.Sprintf("type: sqlite\ndsn: %s\ndisableAutoMigration: true\nmetrics:\n  disable: true\n",
		filepath.Join(t.TempDir(), "test.db")))

	if _, err := NewStorageFactory(config); err == nil || !strings.Contains(err.Error(), "CREATE TABLE") {
		t.Fatalf("expected an error with the pending statements, got %v", err)
	}
	if err := CheckSchema(config); !errors.Is(err, ErrSchemaOutOfDate) {
		t.Fatalf("expected the schema to be out of date, got %v", err)
	}

	if err := MigrateSchema(config); err != nil {
		t.Fatal(err)
	}
	if err := CheckSchema(config); err != nil {
		t.Fatal(err)
	}
	if statements, err := PendingSchemaSQL(config); err != nil || len(statements) != 0 {
		t.Fatalf("expected no pending statements after migrating, got %q, %v", statements, err)
	}

	factory, err := NewStorageFactory(config)
	if err != nil {
		t.Fatal(err)
	}

	// the definition of an existing unique index is not migrated
	const index = "uni_group_version_resource_cluster_namespace_name"
	if err := factory.(*StorageFactory).db.Exec("DROP INDEX " + index + "; CREATE INDEX " + index + " ON resources (name)").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(config); err == nil || errors.Is(err, ErrSchemaOutOfDate) {
		t.Fatalf("expected an error not wrapping ErrSchemaOutOfDate, got %v", err)
	}

	if err := factory.Shutdown(); err != nil {
		t.Fatal(err)
	}
}
