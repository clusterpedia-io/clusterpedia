package internalstorage

import (
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgerrcode"
	"gorm.io/gorm"
	genericstorage "k8s.io/apiserver/pkg/storage"
)

func InterpreResourceError(cluster, name string, err error) error {
	if err == nil {
		return nil
	}

	return InterpreError(fmt.Sprintf("%s/%s", cluster, name), err)
}

func InterpreError(key string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return genericstorage.NewKeyNotFoundError(key, 0)
	}

	// TODO(iceber): add dialector judgment
	mysqlErr := InterpreMysqlError(key, err)
	if mysqlErr != err {
		return mysqlErr
	}

	pgError := InterprePostgresError(key, err)
	if pgError != err {
		return pgError
	}

	return genericstorage.NewInternalError(err.Error())
}

func InterpreMysqlError(key string, err error) error {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return err
	}

	switch mysqlErr.Number {
	case 1062:
		return genericstorage.NewKeyExistsError(key, 0)
	}
	return err
}

func InterprePostgresError(key string, err error) error {
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		return err
	}

	switch pgError.Code {
	case pgerrcode.UniqueViolation:
		return genericstorage.NewKeyExistsError(key, 0)
	}
	return err
}
