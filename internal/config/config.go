// Package config loads the settings of the service from environment variables
package config

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"

	"github.com/caarlos0/env/v11"
)

// The values that APP_ENV accepts
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

const (
	maxPort    = 65535
	maxDBConns = 50
)

// Config holds the settings of the service. The names of the variables are listed in .env.example.
type Config struct {
	AppEnv  string `env:"APP_ENV" envDefault:"development"`
	AppPort uint   `env:"APP_PORT" envDefault:"8080"`

	DBHost     string `env:"DB_HOST,required,notEmpty"`
	DBPort     uint   `env:"DB_PORT" envDefault:"5432"`
	DBName     string `env:"DB_NAME,required,notEmpty"`
	DBUsername string `env:"DB_USERNAME,required,notEmpty"`
	DBPassword string `env:"DB_PASSWORD,required,notEmpty"`
	DBSSLMode  string `env:"DB_SSL_MODE" envDefault:"require"`
	// DBMaxConns is the size of the pool of one copy. The database is shared, so two copies must stay well below its connection limit.
	DBMaxConns uint `env:"DB_MAX_CONNS" envDefault:"8"`
}

// sslModes are the values that PostgreSQL accepts for sslmode
var sslModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

// Load reads the settings from the environment of the process and refuses a value that the service cannot run with
func Load() (Config, error) {
	return parse(nil)
}

// parse reads the settings from environ, or from the environment of the process when environ is nil
func parse(environ map[string]string) (Config, error) {
	var cfg Config

	if err := env.ParseWithOptions(&cfg, env.Options{Environment: environ}); err != nil {
		return Config{}, fmt.Errorf("parse the environment %w", err)
	}

	if !slices.Contains([]string{EnvDevelopment, EnvProduction}, cfg.AppEnv) {
		return Config{}, fmt.Errorf("APP_ENV is %q and must be %s or %s", cfg.AppEnv, EnvDevelopment, EnvProduction)
	}

	if cfg.AppPort == 0 || cfg.AppPort > maxPort {
		return Config{}, fmt.Errorf("APP_PORT is %d and must be between 1 and %d", cfg.AppPort, maxPort)
	}

	if cfg.DBPort == 0 || cfg.DBPort > maxPort {
		return Config{}, fmt.Errorf("DB_PORT is %d and must be between 1 and %d", cfg.DBPort, maxPort)
	}

	if !slices.Contains(sslModes, cfg.DBSSLMode) {
		return Config{}, fmt.Errorf("DB_SSL_MODE is %q and must be one of %v", cfg.DBSSLMode, sslModes)
	}

	if cfg.DBMaxConns == 0 || cfg.DBMaxConns > maxDBConns {
		return Config{}, fmt.Errorf("DB_MAX_CONNS is %d and must be between 1 and %d", cfg.DBMaxConns, maxDBConns)
	}

	return cfg, nil
}

// DatabaseURL returns the connection URL of the database. The user and the password are escaped by the URL type.
func (c Config) DatabaseURL() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.DBUsername, c.DBPassword),
		Host:     net.JoinHostPort(c.DBHost, strconv.FormatUint(uint64(c.DBPort), 10)),
		Path:     "/" + c.DBName,
		RawQuery: url.Values{"sslmode": {c.DBSSLMode}}.Encode(),
	}

	return u.String()
}
