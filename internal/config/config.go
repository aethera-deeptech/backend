// Package config loads the settings of the service from environment variables
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/caarlos0/env/v11"
)

// The values that APP_ENV accepts
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

const (
	maxPort         = 65535
	maxDBConns      = 50
	maxSentryEvents = 1000
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
	// AppOrigin is the origin of the web app. CORS allows only this origin, and the links in the emails point to it.
	AppOrigin string `env:"APP_ORIGIN" envDefault:"http://localhost:5173"`
	// TrustProxy makes the service read the address of the client from ProxyHeader when the request comes from a private or loopback
	// address, which is the reverse proxy on the same host.
	TrustProxy  bool   `env:"TRUST_PROXY" envDefault:"false"`
	ProxyHeader string `env:"PROXY_HEADER" envDefault:"X-Real-IP"`
	// TrustProxyCIDRs narrows the trusted addresses to a list of IPs or CIDR ranges. When it is empty every private and loopback
	// address is trusted, which includes other containers on a shared Docker network.
	TrustProxyCIDRs []string `env:"TRUST_PROXY_CIDRS" envSeparator:","`

	SMTPHost     string `env:"SMTP_HOST"`
	SMTPPort     uint   `env:"SMTP_PORT" envDefault:"465"`
	SMTPUsername string `env:"SMTP_USERNAME"`
	SMTPPassword string `env:"SMTP_PASSWORD"`
	SMTPSecurity string `env:"SMTP_SECURITY" envDefault:"tls"`
	SMTPFromName string `env:"SMTP_FROM_NAME" envDefault:"Aethera"`

	// SentryDSN is where the error reports go, and empty switches them off. SentryEventsPerMinute is the most events that leave in a
	// minute, so a crash loop cannot use up the quota.
	SentryDSN             string `env:"SENTRY_DSN"`
	SentryEnvironment     string `env:"SENTRY_ENVIRONMENT"`
	SentryServerName      string `env:"SENTRY_SERVER_NAME"`
	SentryEventsPerMinute int    `env:"SENTRY_EVENTS_PER_MINUTE" envDefault:"30"`
	SMTPFromAddress       string `env:"SMTP_FROM_ADDRESS"`

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

	origin, err := url.Parse(cfg.AppOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.Path != "" || origin.RawQuery != "" {
		return Config{}, fmt.Errorf("APP_ORIGIN is %q and must be an origin such as https://app.example.com with no path", cfg.AppOrigin)
	}

	if cfg.AppEnv == EnvProduction && origin.Scheme != "https" {
		return Config{}, fmt.Errorf("APP_ORIGIN is %q and must use https in production", cfg.AppOrigin)
	}

	if origin.User != nil || strings.Contains(origin.Host, "*") {
		return Config{}, fmt.Errorf("APP_ORIGIN is %q and must be one exact origin, with no user name and no wildcard", cfg.AppOrigin)
	}

	// a browser sends the origin in lower case and without the default port, so the setting is written the same way
	cfg.AppOrigin = canonicalOrigin(origin)

	// every entry is written in its plain form, because the server compares it as text with the address of the connection
	for i, entry := range cfg.TrustProxyCIDRs {
		if ip, network, err := net.ParseCIDR(entry); err == nil {
			// an IPv4-mapped range shorter than 96 bits is not an IPv4 range, and the server would never match it
			if ones, bits := network.Mask.Size(); bits == 128 && ones < 96 && ip.To4() != nil {
				return Config{}, fmt.Errorf("TRUST_PROXY_CIDRS holds %q, and a mapped IPv4 range needs a prefix of 96 bits or more", entry)
			}

			cfg.TrustProxyCIDRs[i] = network.String()

			continue
		}

		ip := net.ParseIP(entry)
		if ip == nil {
			return Config{}, fmt.Errorf("TRUST_PROXY_CIDRS holds %q, which is not an IP address or a CIDR range", entry)
		}

		cfg.TrustProxyCIDRs[i] = ip.String()
	}

	if !slices.Contains([]string{"tls", "starttls", "none"}, cfg.SMTPSecurity) {
		return Config{}, fmt.Errorf("SMTP_SECURITY is %q and must be tls, starttls or none", cfg.SMTPSecurity)
	}

	if cfg.SMTPPort == 0 || cfg.SMTPPort > maxPort {
		return Config{}, fmt.Errorf("SMTP_PORT is %d and must be between 1 and %d", cfg.SMTPPort, maxPort)
	}

	// without a mail server nobody can verify an email, so a production service that cannot send refuses to start
	if cfg.AppEnv == EnvProduction && (cfg.SMTPHost == "" || cfg.SMTPFromAddress == "") {
		return Config{}, errors.New("SMTP_HOST and SMTP_FROM_ADDRESS are required in production")
	}

	// a copy that runs in development must not send its events to the production environment of the project
	if cfg.SentryEnvironment == "" {
		cfg.SentryEnvironment = cfg.AppEnv
	}

	if cfg.SentryEventsPerMinute < 1 || cfg.SentryEventsPerMinute > maxSentryEvents {
		return Config{}, fmt.Errorf("SENTRY_EVENTS_PER_MINUTE is %d and must be between 1 and %d", cfg.SentryEventsPerMinute, maxSentryEvents)
	}

	if cfg.DBMaxConns == 0 || cfg.DBMaxConns > maxDBConns {
		return Config{}, fmt.Errorf("DB_MAX_CONNS is %d and must be between 1 and %d", cfg.DBMaxConns, maxDBConns)
	}

	return cfg, nil
}

// canonicalOrigin writes an origin the way a browser serializes it
func canonicalOrigin(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}

	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}

	if port != "" {
		host += ":" + port
	}

	return u.Scheme + "://" + host
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
