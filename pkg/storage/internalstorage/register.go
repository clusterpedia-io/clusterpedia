package internalstorage

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v4/stdlib"
	"github.com/jinzhu/configor"
	gmysql "gorm.io/driver/mysql"
	gpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

func init() {
	storage.RegisterStorageFactoryFunc("internal", NewStorageFactory)
}

func NewStorageFactory(configPath string) (storage.StorageFactory, error) {
	cfg := &Config{}
	if err := configor.Load(cfg, configPath); err != nil {
		return nil, err
	}

	var dialector gorm.Dialector
	switch cfg.Type {
	case "mysql":
		mysqlConfig, err := cfg.genMySQLConfig()
		if err != nil {
			return nil, err
		}

		connector, err := mysql.NewConnector(mysqlConfig)
		if err != nil {
			return nil, err
		}

		dialector = gmysql.New(gmysql.Config{Conn: sql.OpenDB(connector)})
	case "postgres":
		pgconfig, err := cfg.genPostgresConfig()
		if err != nil {
			return nil, err
		}

		dialector = gpostgres.New(gpostgres.Config{Conn: stdlib.OpenDB(*pgconfig)})
	default:
		return nil, fmt.Errorf("not support storage type: %s", cfg.Type)
	}

	logger, err := newLogger(cfg)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&Resource{}); err != nil {
		return nil, err
	}

	return &StorageFactory{db}, nil
}

func newLogger(cfg *Config) (logger.Interface, error) {
	if cfg.Log == nil {
		return logger.Discard, nil
	}

	loggerConfig, err := cfg.LoggerConfig()
	if err != nil {
		return nil, err
	}

	var logWriter io.Writer
	if cfg.Log.Stdout {
		logWriter = os.Stdout
	} else {
		if err := os.MkdirAll("/var/log/clusterpedia", 0644); err != nil {
			return nil, fmt.Errorf("mkdir log dir error: %w", err)
		}

		logWriter, err = os.OpenFile("/var/log/clusterpedia/internalstorage.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return nil, fmt.Errorf("open storage log file error: %w", err)
		}
	}

	return logger.New(log.New(logWriter, "", log.LstdFlags), loggerConfig), nil
}
