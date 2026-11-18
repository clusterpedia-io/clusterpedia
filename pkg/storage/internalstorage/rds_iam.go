package internalstorage

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	rdsauth "github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const iamAuthTypeAWSRDS = "aws-rds"

// RDS receives the token as a cleartext password.
var errRDSIAMRequiresTLS = errors.New("iamAuth requires TLS without plaintext fallback")

// rdsIAMAuth sets a short-lived RDS IAM token as the password of every new connection.
type rdsIAMAuth struct {
	region      string
	credentials aws.CredentialsProvider
}

// newRDSIAMAuth returns nil when iamAuth.type is not set.
func (cfg *Config) newRDSIAMAuth(ctx context.Context) (*rdsIAMAuth, error) {
	switch cfg.IAMAuth.Type {
	case "":
		return nil, nil
	case iamAuthTypeAWSRDS:
	default:
		return nil, fmt.Errorf("not support iamAuth type: %s", cfg.IAMAuth.Type)
	}

	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.IAMAuth.Region))
	if err != nil {
		return nil, err
	}
	if awsConfig.Region == "" {
		return nil, errors.New("iamAuth: region is required, set iamAuth.region or AWS_REGION")
	}
	return &rdsIAMAuth{region: awsConfig.Region, credentials: awsConfig.Credentials}, nil
}

func (a *rdsIAMAuth) mysqlBeforeConnect(ctx context.Context, cfg *mysql.Config) error {
	if cfg.TLS == nil || cfg.AllowFallbackToPlaintext {
		return errRDSIAMRequiresTLS
	}

	token, err := rdsauth.BuildAuthToken(ctx, cfg.Addr, a.region, cfg.User, a.credentials)
	if err != nil {
		return err
	}
	cfg.Passwd = token
	// RDS for MySQL reads the token through the mysql_clear_password plugin.
	cfg.AllowCleartextPasswords = true
	return nil
}

func (a *rdsIAMAuth) postgresBeforeConnect(ctx context.Context, cfg *pgx.ConnConfig) error {
	if cfg.TLSConfig == nil || slices.ContainsFunc(cfg.Fallbacks, func(f *pgconn.FallbackConfig) bool { return f.TLSConfig == nil }) {
		return errRDSIAMRequiresTLS
	}

	endpoint := net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
	token, err := rdsauth.BuildAuthToken(ctx, endpoint, a.region, cfg.User, a.credentials)
	if err != nil {
		return err
	}
	cfg.Password = token
	return nil
}
