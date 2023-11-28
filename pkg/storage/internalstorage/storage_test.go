package internalstorage

import (
	"fmt"
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gmysql "gorm.io/driver/mysql"
	gpostgres "gorm.io/driver/postgres"
	gsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	postgresDB     *gorm.DB
	postgresDBMock sqlmock.Sqlmock

	mysqlVersions = []string{"8.0.27", "5.7.22"}
	mysqlDBs      = make(map[string]*gorm.DB, 2)
	mysqlDBMocks  = make(map[string]sqlmock.Sqlmock, 2)
)

func newMockedPostgresDB() (*gorm.DB, sqlmock.Sqlmock, error) {
	mockedDB, mock, err := sqlmock.New()
	if err != nil {
		return nil, nil, fmt.Errorf("sqlmock.New() failed: %w", err)
	}

	gormDB, err := gorm.Open(gpostgres.New(gpostgres.Config{Conn: mockedDB}))
	if err != nil {
		return nil, nil, fmt.Errorf("init postgresDB failed: %w", err)
	}

	return gormDB, mock, nil
}

func newSQLiteDB() (*gorm.DB, func(), error) {
	db, err := gorm.Open(gsqlite.Open("test.db"))
	if err != nil {
		return nil, func() {}, err
	}

	err = db.AutoMigrate(&Resource{})
	if err != nil {
		return nil, func() {}, err
	}

	return db, func() {
		err := os.Remove("test.db")
		if err != nil {
			panic(err)
		}
	}, nil
}

func newMockedMySQLDB(version string) (*gorm.DB, sqlmock.Sqlmock, error) {
	mockedDB, mock, err := sqlmock.New()
	if err != nil {
		return nil, nil, fmt.Errorf("sqlmock.New() failed: %w", err)
	}

	mock.ExpectQuery("SELECT VERSION()").WillReturnRows(sqlmock.NewRows([]string{"VERSION()"}).AddRow(version))

	mysqlDB, err := gorm.Open(gmysql.New(gmysql.Config{Conn: mockedDB}))
	if err != nil {
		return nil, nil, fmt.Errorf("init mysqlDB(%s) failed: %w", version, err)
	}

	return mysqlDB, mock, nil
}

func TestMain(m *testing.M) {
	{
		mockedDB, mock, err := newMockedPostgresDB()
		if err != nil {
			panic(err)
		}

		postgresDB = mockedDB
		postgresDBMock = mock
	}

	for _, version := range mysqlVersions {
		mysqlDB, mock, err := newMockedMySQLDB(version)
		if err != nil {
			panic(err)
		}

		mysqlDBs[version] = mysqlDB
		mysqlDBMocks[version] = mock
	}

	os.Exit(m.Run())
}
