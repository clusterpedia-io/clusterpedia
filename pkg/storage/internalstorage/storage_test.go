package internalstorage

import (
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	gmysql "gorm.io/driver/mysql"
	gpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var (
	postgresDB *gorm.DB

	mysqlVersions = []string{"8.0.27", "5.7.22"}
	mysqlDBs      = make(map[string]*gorm.DB, 2)
)

func init() {
	db, _, err := sqlmock.New()
	if err != nil {
		panic(fmt.Sprintf("sqlmock.New() failed: %v", err))
	}

	postgresDB, err = gorm.Open(gpostgres.New(gpostgres.Config{Conn: db}))
	if err != nil {
		panic(fmt.Sprintf("init postgresDB failed: %v", err))
	}

	for _, version := range mysqlVersions {
		db, mock, err := sqlmock.New()
		if err != nil {
			panic(fmt.Sprintf("sqlmock.New() failed: %v", err))
		}
		mock.ExpectQuery("SELECT VERSION()").WillReturnRows(sqlmock.NewRows([]string{"VERSION()"}).AddRow(version))

		mysqlDB, err := gorm.Open(gmysql.New(gmysql.Config{Conn: db}))
		if err != nil {
			panic(fmt.Sprintf("init mysqlDB(%s) failed: %v", version, err))
		}
		mysqlDBs[version] = mysqlDB
	}
}
