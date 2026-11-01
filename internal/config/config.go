// Package config loads the settings of the service from environment variables
package config

import (
	"fmt"
	"slices"

	"github.com/caarlos0/env/v11"
)

// The values that APP_ENV accepts
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

const maxPort = 65535

// Config holds the settings of the service. The names of the variables are listed in .env.example.
type Config struct {
	AppEnv  string `env:"APP_ENV" envDefault:"development"`
	AppPort uint   `env:"APP_PORT" envDefault:"8080"`
}

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

	return cfg, nil
}
