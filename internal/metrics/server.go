package metrics

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Listener is the metrics endpoint that Serve started
type Listener struct {
	addr   string
	server *http.Server
}

// Addr is the address that the endpoint listens on, empty when it is switched off
func (l *Listener) Addr() string { return l.addr }

// Close stops the endpoint and waits a few seconds for a scrape that is under way
func (l *Listener) Close() {
	if l.server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = l.server.Shutdown(ctx)
}

// Serve answers GET /metrics on address and nothing else. It listens before it returns, so an address that is taken or
// malformed stops the start-up and not just the metrics. An empty address serves nothing.
func (m *Metrics) Serve(address string) (*Listener, error) {
	if address == "" {
		return &Listener{}, nil
	}

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("listen for metrics on %s %w", address, err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		ErrorLog:      log.Default(),
		ErrorHandling: promhttp.ContinueOnError,
	}))

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics listener stopped %v", err)
		}
	}()

	log.Printf("metrics listening on %s", listener.Addr())

	return &Listener{addr: listener.Addr().String(), server: server}, nil
}
