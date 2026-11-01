package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// withDB adds the settings of the database, which have no default, to a test environment
func withDB(environ map[string]string) map[string]string {
	out := map[string]string{"DB_HOST": "db", "DB_NAME": "app", "DB_USERNAME": "user", "DB_PASSWORD": "pass"}
	for k, v := range environ {
		out[k] = v
	}

	return out
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		environ map[string]string
		want    Config
		wantErr string
	}{
		{name: "defaults", environ: withDB(nil), want: Config{
			AppEnv: EnvDevelopment, AppPort: 8080, DBHost: "db", DBPort: 5432, DBName: "app", DBUsername: "user", DBPassword: "pass",
			DBSSLMode: "require", DBMaxConns: 8, AppOrigin: "http://localhost:5173", ProxyHeader: "X-Real-IP", SMTPPort: 465,
			SMTPSecurity: "tls", SMTPFromName: "Aethera", SentryEnvironment: "production", SentryEventsPerMinute: 30,
		}},
		{
			name: "values from the environment",
			environ: withDB(map[string]string{
				"APP_ENV": "production", "APP_PORT": "9090", "DB_SSL_MODE": "disable", "DB_MAX_CONNS": "4", "DB_PORT": "6432",
				"APP_ORIGIN": "https://app.example.com", "SMTP_HOST": "smtp.example.com", "SMTP_FROM_ADDRESS": "no-reply@example.com",
			}),
			want: Config{
				AppEnv: EnvProduction, AppPort: 9090, DBHost: "db", DBPort: 6432, DBName: "app", DBUsername: "user", DBPassword: "pass",
				DBSSLMode: "disable", DBMaxConns: 4, AppOrigin: "https://app.example.com", ProxyHeader: "X-Real-IP",
				SMTPHost: "smtp.example.com", SMTPPort: 465, SMTPSecurity: "tls", SMTPFromName: "Aethera", SentryEnvironment: "production", SentryEventsPerMinute: 30, SMTPFromAddress: "no-reply@example.com",
			},
		},
		{name: "unknown environment", environ: withDB(map[string]string{"APP_ENV": "staging"}), wantErr: "APP_ENV"},
		{name: "port zero", environ: withDB(map[string]string{"APP_PORT": "0"}), wantErr: "APP_PORT"},
		{name: "port too high", environ: withDB(map[string]string{"APP_PORT": "65536"}), wantErr: "APP_PORT"},
		{name: "port is not a number", environ: withDB(map[string]string{"APP_PORT": "abc"}), wantErr: "parse the environment"},
		{name: "origin with a path", environ: withDB(map[string]string{"APP_ORIGIN": "https://app.example.com/app"}), wantErr: "APP_ORIGIN"},
		{name: "origin without a scheme", environ: withDB(map[string]string{"APP_ORIGIN": "app.example.com"}), wantErr: "APP_ORIGIN"},
		{name: "http origin in production", environ: withDB(map[string]string{"APP_ENV": "production", "APP_ORIGIN": "http://app.example.com", "SMTP_HOST": "m", "SMTP_FROM_ADDRESS": "a@b.cd"}), wantErr: "https"},
		{name: "production without a mail server", environ: withDB(map[string]string{"APP_ENV": "production", "APP_ORIGIN": "https://app.example.com"}), wantErr: "SMTP_HOST"},
		{name: "unknown smtp security", environ: withDB(map[string]string{"SMTP_SECURITY": "ssl"}), wantErr: "SMTP_SECURITY"},
		{name: "smtp port zero", environ: withDB(map[string]string{"SMTP_PORT": "0"}), wantErr: "SMTP_PORT"},
		{name: "no database settings", environ: map[string]string{}, wantErr: "DB_HOST"},
		{name: "empty password", environ: withDB(map[string]string{"DB_PASSWORD": ""}), wantErr: "DB_PASSWORD"},
		{name: "a bad proxy list", environ: withDB(map[string]string{"TRUST_PROXY_CIDRS": "10.0.0.0/8,nope"}), wantErr: "TRUST_PROXY_CIDRS"},
		{name: "origin with a user name", environ: withDB(map[string]string{"APP_ORIGIN": "https://user@app.example.com"}), wantErr: "APP_ORIGIN"},
		{name: "no error reports allowed", environ: withDB(map[string]string{"SENTRY_EVENTS_PER_MINUTE": "0"}), wantErr: "SENTRY_EVENTS_PER_MINUTE"},
		{name: "database port zero", environ: withDB(map[string]string{"DB_PORT": "0"}), wantErr: "DB_PORT"},
		{name: "unknown ssl mode", environ: withDB(map[string]string{"DB_SSL_MODE": "yes"}), wantErr: "DB_SSL_MODE"},
		{name: "no connections", environ: withDB(map[string]string{"DB_MAX_CONNS": "0"}), wantErr: "DB_MAX_CONNS"},
		{name: "too many connections", environ: withDB(map[string]string{"DB_MAX_CONNS": "51"}), wantErr: "DB_MAX_CONNS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(tt.environ)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error is %v, want one that contains %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("config is %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestOriginIsWrittenLikeTheBrowserSendsIt(t *testing.T) {
	for in, want := range map[string]string{
		"https://app.example.com:443":  "https://app.example.com",
		"https://APP.Example.com":      "https://app.example.com",
		"http://localhost:80":          "http://localhost",
		"http://localhost:5173":        "http://localhost:5173",
		"https://app.example.com:8443": "https://app.example.com:8443",
		"http://[::1]:5173":            "http://[::1]:5173",
	} {
		got, err := parse(withDB(map[string]string{"APP_ORIGIN": in}))
		if err != nil {
			t.Fatalf("%s was refused %v", in, err)
		}

		if got.AppOrigin != want {
			t.Fatalf("%s became %s, want %s", in, got.AppOrigin, want)
		}
	}
}

func TestAnOriginWithAWildcardIsRefused(t *testing.T) {
	for _, in := range []string{"https://*.example.com", "https://*.example.com:443", "https://app.*.com"} {
		if _, err := parse(withDB(map[string]string{"APP_ORIGIN": in})); err == nil {
			t.Fatalf("%s was accepted", in)
		}
	}
}

func TestTheProxyListIsWrittenInItsPlainForm(t *testing.T) {
	got, err := parse(withDB(map[string]string{"TRUST_PROXY_CIDRS": "::ffff:172.18.0.1,10.1.2.3/8,2001:DB8::1"}))
	if err != nil || !reflect.DeepEqual(got.TrustProxyCIDRs, []string{"172.18.0.1", "10.0.0.0/8", "2001:db8::1"}) {
		t.Fatalf("the list is %v (%v)", got.TrustProxyCIDRs, err)
	}
}

func TestTheProxyListHoldsAddressesAndRanges(t *testing.T) {
	got, err := parse(withDB(map[string]string{"TRUST_PROXY_CIDRS": "172.18.0.1,10.0.0.0/8"}))
	if err != nil || !reflect.DeepEqual(got.TrustProxyCIDRs, []string{"172.18.0.1", "10.0.0.0/8"}) {
		t.Fatalf("the list is %v (%v)", got.TrustProxyCIDRs, err)
	}
}

func TestLoadReadsTheProcessEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", EnvProduction)
	t.Setenv("APP_PORT", "8181")
	t.Setenv("DB_HOST", "db")
	t.Setenv("DB_NAME", "app")
	t.Setenv("DB_USERNAME", "user")
	t.Setenv("DB_PASSWORD", "pass")
	t.Setenv("APP_ORIGIN", "https://app.example.com")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_FROM_ADDRESS", "no-reply@example.com")

	got, err := Load()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if got.AppEnv != EnvProduction || got.AppPort != 8181 {
		t.Fatalf("config is %+v", got)
	}
}

func TestDatabaseURLEscapesTheCredentials(t *testing.T) {
	cfg := Config{DBHost: "db.internal", DBPort: 5432, DBName: "app", DBUsername: "us er", DBPassword: "p@ss/w:rd", DBSSLMode: "disable"}

	want := "postgres://us%20er:p%40ss%2Fw%3Ard@db.internal:5432/app?sslmode=disable"
	if got := cfg.DatabaseURL(); got != want {
		t.Fatalf("url is %s, want %s", got, want)
	}
}

// TestEnvExampleIsValid reads .env.example, so the file that the docs point to cannot drift away from what the loader accepts
func TestEnvExampleIsValid(t *testing.T) {
	raw, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatalf("read the example %v", err)
	}

	environ := map[string]string{}

	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("the line %q has no =", line)
		}

		environ[key] = value
	}

	if _, err := parse(environ); err != nil {
		t.Fatalf("the example is not accepted %v", err)
	}
}
