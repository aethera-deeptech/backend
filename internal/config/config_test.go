package config

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		environ map[string]string
		want    Config
		wantErr string
	}{
		{name: "defaults", environ: map[string]string{}, want: Config{AppEnv: EnvDevelopment, AppPort: 8080}},
		{
			name:    "values from the environment",
			environ: map[string]string{"APP_ENV": "production", "APP_PORT": "9090"},
			want:    Config{AppEnv: EnvProduction, AppPort: 9090},
		},
		{name: "unknown environment", environ: map[string]string{"APP_ENV": "staging"}, wantErr: "APP_ENV"},
		{name: "port zero", environ: map[string]string{"APP_PORT": "0"}, wantErr: "APP_PORT"},
		{name: "port too high", environ: map[string]string{"APP_PORT": "65536"}, wantErr: "APP_PORT"},
		{name: "port is not a number", environ: map[string]string{"APP_PORT": "abc"}, wantErr: "parse the environment"},
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

			if got != tt.want {
				t.Fatalf("config is %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadReadsTheProcessEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", EnvProduction)
	t.Setenv("APP_PORT", "8181")

	got, err := Load()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if got.AppEnv != EnvProduction || got.AppPort != 8181 {
		t.Fatalf("config is %+v", got)
	}
}
