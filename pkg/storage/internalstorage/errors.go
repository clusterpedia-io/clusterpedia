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

func InterpretResourceDBError(cluster, name string, err error) error {
	if err == nil {
		return nil
	}

	return InterpretDBError(fmt.Sprintf("%s/%s", cluster, name), err)
}

func InterpretDBError(key string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return genericstorage.NewKeyNotFoundError(key, 0)
	}

	// TODO(iceber): add dialector judgment
	mysqlErr := InterpretMysqlError(key, err)
	if mysqlErr != err {
		return mysqlErr
	}

	pgError := InterpretPostgresError(key, err)
	if pgError != err {
		return pgError
	}

	return err
}

func InterpretMysqlError(key string, err error) error {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return err
	}

	switch mysqlErr.Number {
	case 1062:
		return genericstorage.NewKeyExistsError(key, 0)
	case 1040:
		// klog.Error("too many connections")
	}
	return err
}

func InterpretPostgresError(key string, err error) error {
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
