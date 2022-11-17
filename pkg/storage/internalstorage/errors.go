package internalstorage

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgerrcode"
	"gorm.io/gorm"
	genericstorage "k8s.io/apiserver/pkg/storage"

	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

var recoverableErrors = []error{
	io.ErrClosedPipe,
	io.ErrUnexpectedEOF,
	os.ErrDeadlineExceeded,
	syscall.ECONNREFUSED,
	driver.ErrBadConn,
}

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

	if _, isNetError := err.(net.Error); isNetError {
		return storage.NewRecoverableException(err)
	}

	if os.IsTimeout(err) {
		return storage.NewRecoverableException(err)
	}

	for _, re := range recoverableErrors {
		if errors.Is(err, re) {
			return storage.NewRecoverableException(err)
		}
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
	// ER_SERVER_SHUTDOWN: Server shutdown in progress
	case 1053:
		return storage.NewRecoverableException(err)
	case 1062:
		return genericstorage.NewKeyExistsError(key, 0)
	case 1040:
		// klog.Error("too many connections")
	}
	return err
}

func InterpretPostgresError(key string, err error) error {
	if pgconn.Timeout(err) {
		return storage.NewRecoverableException(err)
	}

	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		return err
	}

	switch pgError.Code {
	case pgerrcode.UniqueViolation:
		return genericstorage.NewKeyExistsError(key, 0)
	case pgerrcode.AdminShutdown:
		return storage.NewRecoverableException(err)
	}
	return err
}
