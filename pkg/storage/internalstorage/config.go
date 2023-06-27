package internalstorage

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v4"
	"gopkg.in/natefinch/lumberjack.v2"
	"gorm.io/gorm/logger"
	"k8s.io/klog/v2"
)

const (
	defaultMaxIdleConns     = 5
	defaultMaxOpenConns     = 40
	defaultConnMaxLifetime  = time.Hour
	databasePasswordEnvName = "DB_PASSWORD"
)

type Config struct {
	Type string `env:"DB_TYPE" required:"true"`
	DSN  string `env:"DB_DSN"`

	Network string `env:"DB_NETWORK"` // Network type, either tcp or unix, Default is tcp
	Host    string `env:"DB_HOST"`    // TCP host:port or Unix socket depending on Network
	Port    string `env:"DB_PORT"`

	User     string `env:"DB_USER"`
	Password string `env:"DB_PASSWORD"`
	Database string `env:"DB_DATABASE"`

	SSLMode      string `yaml:"sslMode"`
	CertFile     string `yaml:"sslCertFile"`
	KeyFile      string `yaml:"sslKeyFile"`
	RootCertFile string `yaml:"sslRootCertFile"`

	ConnPool ConnPoolConfig `yaml:"connPool"`

	MySQL    *MySQLConfig    `yaml:"mysql"`
	Postgres *PostgresConfig `yaml:"postgres"`

	Params map[string]string `yaml:"params"`

	Log *LogConfig `yaml:"log"`
}

type LogConfig struct {
	Stdout                    bool               `yaml:"stdout"`
	Level                     string             `yaml:"level"`
	Colorful                  bool               `yaml:"colorful"`
	SlowThreshold             time.Duration      `yaml:"slowThreshold" default:"200ms"`
	IgnoreRecordNotFoundError bool               `yaml:"ignoreRecordNotFoundError"`
	Logger                    *lumberjack.Logger `yaml:"logger"`
}

type MySQLConfig struct {
	DialTimeout  *time.Duration `yaml:"dialTimeout"`
	ReadTimeout  *time.Duration `yaml:"readTimeout"`
	WriteTimeout *time.Duration `yaml:"writeTimeout"`

	ServerPubKey     *string `yaml:"serverPubKey"`
	Loc              *string `yaml:"loc"`
	MaxAllowedPacket *int    `yaml:"maxAllowedPacket"`

	AllowAllFiles           *bool `yaml:"allowAllFiles"`           // Allow all files to be used with LOAD DATA LOCAL INFILE
	AllowCleartextPasswords *bool `yaml:"allowCleartextPasswords"` // Allows the cleartext client side plugin
	AllowNativePasswords    *bool `yaml:"allowNativePasswords"`    // Allows the native password authentication method
	AllowOldPasswords       *bool `yaml:"allowOldPasswords"`       // Allows the old insecure password method
	CheckConnLiveness       *bool `yaml:"checkConnLiveness"`       // Check connections for liveness before using them
	ClientFoundRows         *bool `yaml:"clientFoundRows"`         // Return number of matching rows instead of rows changed
	ColumnsWithAlias        *bool `yaml:"columnsWithAlias"`        // Prepend table alias to column names
	InterpolateParams       *bool `yaml:"interpolateParams"`       // Interpolate placeholders into query string
	MultiStatements         *bool `yaml:"multiStatements"`         // Allow multiple statements in one query
	ParseTime               *bool `yaml:"parseTime"`               // Parse time values to time.Time
	RejectReadOnly          *bool `yaml:"rejectReadOnly"`          // Reject read-only connections

	RecoverableErrNumbers []int `yaml:"recoverableErrNumbers"`
}

type PostgresConfig struct {
	RecoverableErrCodes []string `yaml:"recoverableErrCodes"`
}

type ConnPoolConfig struct {
	MaxIdleConns    int           `yaml:"maxIdleConns"`
	MaxOpenConns    int           `yaml:"maxOpenConns"`
	ConnMaxLifetime time.Duration `yaml:"connMaxLifetime"`
}

func (cfg *Config) LoggerConfig() (logger.Config, error) {
	if cfg.Log == nil {
		return logger.Config{}, nil
	}

	var logLevel logger.LogLevel
	switch cfg.Log.Level {
	case "Silent":
		logLevel = logger.Silent
	case "Error":
		logLevel = logger.Error
	case "", "Warn":
		logLevel = logger.Warn
	case "Info":
		logLevel = logger.Info
	default:
		return logger.Config{}, errors.New("log level must be one of [Silent, Error, Warn, Info], Default is 'Warn'")
	}

	return logger.Config{
		SlowThreshold:             cfg.Log.SlowThreshold,
		LogLevel:                  logLevel,
		IgnoreRecordNotFoundError: cfg.Log.IgnoreRecordNotFoundError,
		Colorful:                  cfg.Log.Colorful,
	}, nil
}

func (cfg *Config) getConnPoolConfig() (ConnPoolConfig, error) {
	connPool := ConnPoolConfig{
		MaxIdleConns:    cfg.ConnPool.MaxIdleConns,
		MaxOpenConns:    cfg.ConnPool.MaxOpenConns,
		ConnMaxLifetime: cfg.ConnPool.ConnMaxLifetime,
	}
	if connPool.MaxIdleConns <= 0 {
		connPool.MaxIdleConns = defaultMaxIdleConns
	}
	if connPool.MaxOpenConns <= 0 {
		connPool.MaxOpenConns = defaultMaxOpenConns
	}
	lifeTimeSeconds := connPool.ConnMaxLifetime.Seconds()
	if lifeTimeSeconds > 0 && lifeTimeSeconds < defaultConnMaxLifetime.Seconds() {
		connPool.ConnMaxLifetime = defaultConnMaxLifetime
	}

	if connPool.MaxOpenConns < connPool.MaxIdleConns {
		return ConnPoolConfig{}, fmt.Errorf("connPool maxIdleConns is bigger than maxOpenConns, config detail: %v, please check the config", connPool)
	}
	return connPool, nil
}

func (cfg *Config) genMySQLConfig() (*mysql.Config, error) {
	if cfg.DSN != "" {
		mysqlConfig, err := mysql.ParseDSN(cfg.DSN)
		if err != nil {
			return nil, err
		}
		if mysqlConfig.Passwd == "" {
			mysqlConfig.Passwd = os.Getenv(databasePasswordEnvName)
		}
		mysqlConfig.ParseTime = true
		return mysqlConfig, nil
	}

	if cfg.Database == "" {
		return nil, errors.New("mysql: database name is required")
	}

	tlsConfig, err := configTLS(cfg.Host, cfg.SSLMode, cfg.RootCertFile, cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, err
	}
	if err := mysql.RegisterTLSConfig(cfg.SSLMode, tlsConfig); err != nil {
		return nil, err
	}

	mysqlconfig := mysql.NewConfig()
	mysqlconfig.User = cfg.User
	mysqlconfig.Passwd = cfg.Password
	mysqlconfig.Addr = net.JoinHostPort(cfg.Host, cfg.Port)
	mysqlconfig.DBName = cfg.Database
	mysqlconfig.TLSConfig = cfg.SSLMode
	mysqlconfig.Params = cfg.Params
	if cfg.MySQL == nil {
		// https://github.com/go-gorm/gorm/issues/958
		// https://github.com/go-sql-driver/mysql/issues/9
		// mysqlconfig.ParseTime defaults to true
		mysqlconfig.ParseTime = true
		return mysqlconfig, nil
	}

	if cfg.MySQL.ServerPubKey != nil {
		mysqlconfig.ServerPubKey = *cfg.MySQL.ServerPubKey
	}
	if cfg.MySQL.DialTimeout != nil {
		mysqlconfig.Timeout = *cfg.MySQL.DialTimeout
	}
	if cfg.MySQL.ReadTimeout != nil {
		mysqlconfig.ReadTimeout = *cfg.MySQL.ReadTimeout
	}
	if cfg.MySQL.WriteTimeout != nil {
		mysqlconfig.WriteTimeout = *cfg.MySQL.WriteTimeout
	}
	if cfg.MySQL.MaxAllowedPacket != nil {
		mysqlconfig.MaxAllowedPacket = *cfg.MySQL.MaxAllowedPacket
	}
	if cfg.MySQL.MultiStatements != nil {
		mysqlconfig.MultiStatements = *cfg.MySQL.MultiStatements
	}
	if cfg.MySQL.AllowAllFiles != nil {
		mysqlconfig.AllowAllFiles = *cfg.MySQL.AllowAllFiles
	}
	if cfg.MySQL.AllowCleartextPasswords != nil {
		mysqlconfig.AllowCleartextPasswords = *cfg.MySQL.AllowCleartextPasswords
	}
	if cfg.MySQL.AllowNativePasswords != nil {
		mysqlconfig.AllowNativePasswords = *cfg.MySQL.AllowNativePasswords
	}
	if cfg.MySQL.AllowOldPasswords != nil {
		mysqlconfig.AllowOldPasswords = *cfg.MySQL.AllowOldPasswords
	}
	if cfg.MySQL.CheckConnLiveness != nil {
		mysqlconfig.CheckConnLiveness = *cfg.MySQL.CheckConnLiveness
	}
	if cfg.MySQL.ClientFoundRows != nil {
		mysqlconfig.ClientFoundRows = *cfg.MySQL.ClientFoundRows
	}
	if cfg.MySQL.ColumnsWithAlias != nil {
		mysqlconfig.ColumnsWithAlias = *cfg.MySQL.ColumnsWithAlias
	}
	if cfg.MySQL.InterpolateParams != nil {
		mysqlconfig.InterpolateParams = *cfg.MySQL.InterpolateParams
	}
	if cfg.MySQL.ParseTime != nil && !*cfg.MySQL.ParseTime {
		klog.Warningln("Mysql query param parseTime=false has been ignored, and set to true")
	}
	mysqlconfig.ParseTime = true
	if cfg.MySQL.RejectReadOnly != nil {
		mysqlconfig.RejectReadOnly = *cfg.MySQL.RejectReadOnly
	}
	return mysqlconfig, nil
}

func (cfg *Config) genPostgresConfig() (*pgx.ConnConfig, error) {
	if cfg.DSN != "" {
		if !strings.Contains(cfg.DSN, "password") && os.Getenv(databasePasswordEnvName) != "" {
			cfg.DSN = cfg.DSN + fmt.Sprintf(" password=%s", os.Getenv(databasePasswordEnvName))
		}
		return pgx.ParseConfig(cfg.DSN)
	}

	if cfg.Database == "" {
		return nil, errors.New("postgres: database name is required")
	}

	var names []string
	if cfg.Host != "" {
		names = append(names, fmt.Sprintf("host=%s", cfg.Host))
	}
	if cfg.Port != "" {
		names = append(names, fmt.Sprintf("port=%s", cfg.Port))
	}
	if cfg.User != "" {
		names = append(names, fmt.Sprintf("user=%s", cfg.User))
	}
	if cfg.Password != "" {
		names = append(names, fmt.Sprintf("password=%s", cfg.Password))
	}
	if cfg.Database != "" {
		names = append(names, fmt.Sprintf("dbname=%s", cfg.Database))
	}
	if cfg.SSLMode != "" {
		names = append(names, fmt.Sprintf("sslmode=%s", cfg.SSLMode))
	}
	if cfg.CertFile != "" {
		names = append(names, fmt.Sprintf("sslcert=%s", cfg.CertFile))
	}
	if cfg.KeyFile != "" {
		names = append(names, fmt.Sprintf("sslcert=%s", cfg.KeyFile))
	}
	if cfg.RootCertFile != "" {
		names = append(names, fmt.Sprintf("sslrootcert=%s", cfg.RootCertFile))
	}
	for key, value := range cfg.Params {
		names = append(names, fmt.Sprintf("%s=%s", key, value))
	}
	dns := strings.Join(names, " ")
	return pgx.ParseConfig(dns)
}

func (cfg *Config) genSQLiteDSN() (string, error) {
	if cfg.DSN == "" {
		return "", errors.New("sqlite: dsn is required")
	}
	return cfg.DSN, nil
}

func (cfg *Config) addMysqlErrorNumbers() {
	if cfg.MySQL != nil {
		for _, errCode := range cfg.MySQL.RecoverableErrNumbers {
			recoverableMysqlErrNumbers.Store(errCode, struct{}{})
		}
	}
}

func (cfg *Config) addPostgresErrorCodes() {
	if cfg.Postgres != nil {
		for _, errCode := range cfg.Postgres.RecoverableErrCodes {
			recoverablePostgresErrCodes.Store(errCode, struct{}{})
		}
	}
}

func configTLS(host, sslmode, sslrootcert, sslcert, sslkey string) (*tls.Config, error) {
	tlsConfig := &tls.Config{}
	switch sslmode {
	case "false", "", "disable":
		return nil, nil

	case "skip-verify", "allow", "prefer":
		tlsConfig.InsecureSkipVerify = true
		return tlsConfig, nil

	case "require":
		if sslrootcert != "" {
			tlsConfig.InsecureSkipVerify = true
			return tlsConfig, nil
		}

		fallthrough
	case "verify-ca":
		// Don't perform the default certificate verification because it
		// will verify the hostname. Instead, verify the server's
		// certificate chain ourselves in VerifyPeerCertificate and
		// ignore the server name. This emulates libpq's verify-ca
		// behavior.
		//
		// See https://github.com/golang/go/issues/21971#issuecomment-332693931
		// and https://pkg.go.dev/crypto/tls?tab=doc#example-Config-VerifyPeerCertificate
		// for more info.
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyPeerCertificate = func(certificates [][]byte, _ [][]*x509.Certificate) error {
			certs := make([]*x509.Certificate, len(certificates))
			for i, asn1Data := range certificates {
				cert, err := x509.ParseCertificate(asn1Data)
				if err != nil {
					return errors.New("failed to parse certificate from server: " + err.Error())
				}
				certs[i] = cert
			}

			// Leave DNSName empty to skip hostname verification.
			opts := x509.VerifyOptions{
				Roots:         tlsConfig.RootCAs,
				Intermediates: x509.NewCertPool(),
			}
			// Skip the first cert because it's the leaf. All others
			// are intermediates.
			for _, cert := range certs[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := certs[0].Verify(opts)
			return err
		}
	case "verify-full":
		if host != "" {
			tlsConfig.ServerName = host
		}
	default:
		return nil, errors.New("sslmode is invalid")
	}

	if sslrootcert != "" {
		caCertPool := x509.NewCertPool()

		caPath := sslrootcert
		caCert, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("unable to read CA file: %w", err)
		}

		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, errors.New("unable to add CA to cert pool")
		}

		tlsConfig.RootCAs = caCertPool
		tlsConfig.ClientCAs = caCertPool
	}

	if (sslcert != "" && sslkey == "") || (sslcert == "" && sslkey != "") {
		return nil, errors.New(`both "sslcert" and "sslkey" are required`)
	}

	if sslcert != "" && sslkey != "" {
		cert, err := tls.LoadX509KeyPair(sslcert, sslkey)
		if err != nil {
			return nil, fmt.Errorf("unable to read cert: %w", err)
		}

		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
