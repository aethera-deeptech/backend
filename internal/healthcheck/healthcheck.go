// Package healthcheck probes the local server so container health checks work in an image without curl
package healthcheck

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultPort = "8080"
	timeout     = 3 * time.Second
)

// URL returns the health endpoint on loopback, a port that is not a number falls back to the default
func URL(port string) string {
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		port = defaultPort
	}

	return fmt.Sprintf("http://127.0.0.1:%s/healthz", port)
}

// Check calls the health endpoint and fails unless it answers 200
func Check(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("build request %w", err)
	}

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call %s %w", url, err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", url, res.StatusCode)
	}

	return nil
}

// Run is the healthcheck command and returns its exit code, 0 when the server is healthy and 1 otherwise
func Run(port string) int {
	client := &http.Client{Timeout: timeout}

	if err := Check(context.Background(), client, URL(port)); err != nil {
		return 1
	}

	return 0
}
