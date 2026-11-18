package internalstorage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRDSIAMTokenPattern = `\?Action=connect&DBUser=clusterpedia&.*X-Amz-Credential=AKID%2F\d{8}%2Fus-west-2%2Frds-db%2Faws4_request`

func newTestRDSIAMAuth() *rdsIAMAuth {
	return &rdsIAMAuth{
		region: "us-west-2",
		credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET"}, nil
		}),
	}
}

// isolateAWSEnv keeps the AWS SDK away from the host's configuration.
func isolateAWSEnv(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("AWS_CONFIG_FILE", missing)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", missing)
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
}

func TestNewRDSIAMAuth(t *testing.T) {
	isolateAWSEnv(t)

	tests := []struct {
		name       string
		auth       IAMAuthConfig
		envRegion  string
		wantRegion string
		wantErr    bool
	}{
		{name: "disabled"},
		{name: "unknown type", auth: IAMAuthConfig{Type: "gcp-cloudsql"}, wantErr: true},
		{name: "configured region", auth: IAMAuthConfig{Type: iamAuthTypeAWSRDS, Region: "us-west-2"}, envRegion: "eu-west-1", wantRegion: "us-west-2"},
		{name: "environment region", auth: IAMAuthConfig{Type: iamAuthTypeAWSRDS}, envRegion: "eu-west-1", wantRegion: "eu-west-1"},
		{name: "missing region", auth: IAMAuthConfig{Type: iamAuthTypeAWSRDS}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AWS_REGION", tt.envRegion)

			iamAuth, err := (&Config{IAMAuth: tt.auth}).newRDSIAMAuth(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantRegion == "" {
				assert.Nil(t, iamAuth)
				return
			}
			assert.Equal(t, tt.wantRegion, iamAuth.region)
		})
	}
}

// The hooks reject the connection before dialing, so no database is needed.
func TestNewStorageFactoryRDSIAMRequiresTLS(t *testing.T) {
	isolateAWSEnv(t)

	for _, dbType := range []string{"mysql", "postgres"} {
		t.Run(dbType, func(t *testing.T) {
			config := fmt.Sprintf(`
type: %s
host: db.example.com
port: 3306
user: clusterpedia
database: clusterpedia
iamAuth:
  type: aws-rds
  region: us-west-2
`, dbType)
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

			_, err := NewStorageFactory(configPath)
			require.ErrorIs(t, err, errRDSIAMRequiresTLS)
		})
	}
}

func TestRDSIAMAuthMySQLBeforeConnect(t *testing.T) {
	tests := []struct {
		name    string
		tls     string
		wantErr bool
	}{
		{name: "tls", tls: "true"},
		{name: "plaintext", tls: "false", wantErr: true},
		{name: "plaintext fallback", tls: "preferred", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := mysql.ParseDSN("clusterpedia@tcp(db.example.com:3306)/clusterpedia?tls=" + tt.tls)
			require.NoError(t, err)

			err = newTestRDSIAMAuth().mysqlBeforeConnect(context.Background(), cfg)
			if tt.wantErr {
				require.ErrorIs(t, err, errRDSIAMRequiresTLS)
				return
			}
			require.NoError(t, err)
			assert.Regexp(t, `^db\.example\.com:3306`+testRDSIAMTokenPattern, cfg.Passwd)
			assert.True(t, cfg.AllowCleartextPasswords)
		})
	}
}

func TestRDSIAMAuthPostgresBeforeConnect(t *testing.T) {
	tests := []struct {
		name    string
		sslmode string
		wantErr bool
	}{
		{name: "tls", sslmode: "require"},
		{name: "plaintext", sslmode: "disable", wantErr: true},
		{name: "plaintext fallback", sslmode: "prefer", wantErr: true},
		{name: "plaintext first", sslmode: "allow", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := pgx.ParseConfig("host=db.example.com port=5432 user=clusterpedia dbname=clusterpedia sslmode=" + tt.sslmode)
			require.NoError(t, err)

			err = newTestRDSIAMAuth().postgresBeforeConnect(context.Background(), cfg)
			if tt.wantErr {
				require.ErrorIs(t, err, errRDSIAMRequiresTLS)
				return
			}
			require.NoError(t, err)
			assert.Regexp(t, `^db\.example\.com:5432`+testRDSIAMTokenPattern, cfg.Password)
		})
	}
}
