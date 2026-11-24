package internalstorage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strings"

	gmysql "gorm.io/driver/mysql"
	gpostgres "gorm.io/driver/postgres"
	gsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ErrSchemaOutOfDate is wrapped by the errors reporting that the schema of the database is out of date.
var ErrSchemaOutOfDate = errors.New("the schema is out of date")

// SchemaSQL returns the statements that create the schema from scratch, without connecting to the database.
func SchemaSQL(dbType string) ([]string, error) {
	// Creating the table doesn't query the database, so mysql and postgres need no underlying ConnPool.
	var dialector gorm.Dialector
	switch dbType {
	case "mysql":
		dialector = gmysql.New(gmysql.Config{Conn: &schemaRecorder{}, SkipInitializeWithVersion: true})
	case "postgres":
		dialector = gpostgres.New(gpostgres.Config{Conn: &schemaRecorder{}})
	case "sqlite", "sqlite3":
		dialector = gsqlite.Open(":memory:")
	default:
		return nil, fmt.Errorf("not support storage type: %s", dbType)
	}

	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, err
	}
	defer closeDB(db)

	return recordSchemaSQL(db, false, func(tx *gorm.DB) error { return tx.Migrator().CreateTable(&Resource{}) })
}

// CheckSchema returns an error wrapping ErrSchemaOutOfDate if the schema of the database is out of date.
func CheckSchema(configPath string) error {
	db, err := openSchemaDB(configPath)
	if err != nil {
		return err
	}
	defer closeDB(db)

	return checkSchema(db)
}

// PendingSchemaSQL returns the statements required to bring the schema of the database up to date.
func PendingSchemaSQL(configPath string) ([]string, error) {
	db, err := openSchemaDB(configPath)
	if err != nil {
		return nil, err
	}
	defer closeDB(db)

	return pendingSchemaSQL(db)
}

// MigrateSchema brings the schema of the database up to date, even if the auto migration is disabled.
func MigrateSchema(configPath string) error {
	db, err := openSchemaDB(configPath)
	if err != nil {
		return err
	}
	defer closeDB(db)

	if err := db.AutoMigrate(&Resource{}); err != nil {
		return err
	}

	// gorm doesn't always converge, e.g. it never restores a dropped empty string default on postgres.
	if err := checkSchema(db); err != nil {
		// ErrSchemaOutOfDate isn't wrapped, migrating again doesn't help.
		return fmt.Errorf("migrated, but %v", err)
	}
	return nil
}

// openSchemaDB opens the database without the configured logger, which may write to stdout like the statements.
func openSchemaDB(configPath string) (*gorm.DB, error) {
	_, db, err := openDB(configPath)
	if err != nil {
		return nil, err
	}
	db.Logger = logger.Discard
	return db, nil
}

func checkSchema(db *gorm.DB) error {
	statements, err := pendingSchemaSQL(db)
	if err != nil {
		return err
	}
	if len(statements) != 0 {
		return fmt.Errorf("%w, pending statements:\n%s;", ErrSchemaOutOfDate, strings.Join(statements, ";\n"))
	}
	return checkUniqueIndexes(db)
}

// checkUniqueIndexes checks the definition of the unique indexes, the migration only checks that they exist.
func checkUniqueIndexes(db *gorm.DB) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&Resource{}); err != nil {
		return err
	}

	// NOTE: gorm doesn't scope the indexes of postgres to the schema, a same-named index in another schema can change the result.
	indexes, err := db.Migrator().GetIndexes(&Resource{})
	if err != nil {
		return err
	}

	for _, expected := range stmt.Schema.ParseIndexes() {
		if expected.Class != "UNIQUE" {
			continue
		}

		var columns []string
		for _, field := range expected.Fields {
			columns = append(columns, field.DBName)
		}

		for _, index := range indexes {
			if index.Name() != expected.Name {
				continue
			}

			// postgres returns the columns in the order of the table, once for every schema containing the table.
			actual := slices.Compact(slices.Sorted(slices.Values(index.Columns())))
			if unique, _ := index.Unique(); !unique || !slices.Equal(actual, slices.Sorted(slices.Values(columns))) {
				return fmt.Errorf("%w, the index %s is not a unique index on (%s)", ErrSchemaOutOfDate, expected.Name, strings.Join(columns, ", "))
			}
		}
	}
	return nil
}

func pendingSchemaSQL(db *gorm.DB) ([]string, error) {
	migrate := func(tx *gorm.DB) error { return tx.AutoMigrate(&Resource{}) }
	if db.Dialector.Name() != "sqlite" {
		return recordSchemaSQL(db, false, migrate)
	}

	// sqlite alters a column by rebuilding the table, and the statements after it depend on the rebuilt table,
	// so the statements are executed on a copy of the schema.
	schema, err := copySQLiteSchema(db)
	if err != nil {
		return nil, err
	}
	defer closeDB(schema)

	return recordSchemaSQL(schema, true, migrate)
}

// copySQLiteSchema returns an in-memory database with the schema of db and without its data.
func copySQLiteSchema(db *gorm.DB) (*gorm.DB, error) {
	var statements []string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY type = 'table' DESC").Scan(&statements).Error; err != nil {
		return nil, err
	}

	schema, err := gorm.Open(gsqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, err
	}
	sqlDB, err := schema.DB()
	if err != nil {
		return nil, err
	}
	// Every connection to :memory: opens its own database.
	sqlDB.SetMaxOpenConns(1)

	for _, statement := range statements {
		if err := schema.Exec(statement).Error; err != nil {
			sqlDB.Close()
			return nil, err
		}
	}
	return schema, nil
}

// recordSchemaSQL returns the statements that migrate executes, they are only sent to the database if execute is set.
func recordSchemaSQL(db *gorm.DB, execute bool, migrate func(tx *gorm.DB) error) ([]string, error) {
	recorder := &schemaRecorder{ConnPool: db.ConnPool, dialector: db.Dialector, execute: execute}

	// Passing the context gives the session its own statement, so the recorder doesn't leak into db.
	tx := db.Session(&gorm.Session{NewDB: true, Context: db.Statement.Context, DisableNestedTransaction: true})
	tx.ConnPool = recorder
	tx.Statement.ConnPool = recorder

	if err := migrate(tx); err != nil {
		return nil, err
	}
	return recorder.statements, nil
}

// schemaRecorder records the statements to execute and only sends them to the database if execute is set,
// queries are always answered by the underlying ConnPool.
type schemaRecorder struct {
	gorm.ConnPool

	dialector  gorm.Dialector
	execute    bool
	statements []string
}

func (r *schemaRecorder) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	r.statements = append(r.statements, r.dialector.Explain(query, args...))
	if r.execute {
		return r.ConnPool.ExecContext(ctx, query, args...)
	}
	return driver.RowsAffected(0), nil
}

// Commit and Rollback make gorm treat the recorder as an open transaction instead of beginning a real one.
func (r *schemaRecorder) Commit() error { return nil }

func (r *schemaRecorder) Rollback() error { return nil }

func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close()
	}
}
