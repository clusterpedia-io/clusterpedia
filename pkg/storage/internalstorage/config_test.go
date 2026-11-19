package internalstorage

import (
	"testing"

	"github.com/jinzhu/configor"
)

func TestGenPostgresConfigPreservesPassword(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
	}{
		{name: "plain", password: "test-password"},
		{name: "spaces", password: " test password "},
		{name: "quote and backslash", password: `'test\password\`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DB_DSN", "")
			t.Setenv(databasePasswordEnvName, tc.password)

			// Load the password through the same environment binding used for a Secret.
			cfg := &Config{
				Type:     "postgres",
				Host:     "127.0.0.1",
				Port:     "5432",
				User:     "clusterpedia",
				Database: "clusterpedia",
				SSLMode:  "disable",
			}
			if err := configor.Load(cfg); err != nil {
				t.Fatalf("load storage configuration: %v", err)
			}
			parsed, err := cfg.genPostgresConfig()
			if err != nil {
				t.Fatalf("parse PostgreSQL connection settings: %v", err)
			}
			if parsed.Password != tc.password {
				t.Fatal("parsed password differs from the Secret value")
			}
		})
	}
}
