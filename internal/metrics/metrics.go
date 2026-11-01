// Package metrics counts what the API does and serves it in the Prometheus text format on a listener of its own. The listener is never
// the public one, so what it shows (route names, error rates, pool sizes) cannot be read from the internet, and a scraper reaches it
// only through the private network of the deployment.
package metrics

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/aethera-deeptech/backend/internal/api"
)

// Unknown is the label of a request whose route or method is not one the API registered. A label that came from the client would let
// anyone fill the memory of the process and of the scraper with series, so only names known at start-up are used.
const Unknown = "unmatched"

const namespace = "aethera"

// rateLimitedCode is the code of every 429 that a limiter gives
const rateLimitedCode = "rate_limited"

// Metrics holds the collectors and the registry that the listener serves
type Metrics struct {
	registry *prometheus.Registry

	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
	limited  *prometheus.CounterVec

	// routes is every route the API registered, set once before the first request and only read after it
	routes map[string]struct{}
	now    func() time.Time
}

// methods are the HTTP methods that get a label of their own, any other one is counted as OTHER
var methods = map[string]struct{}{
	fiber.MethodGet: {}, fiber.MethodPost: {}, fiber.MethodPut: {}, fiber.MethodPatch: {}, fiber.MethodDelete: {},
	fiber.MethodHead: {}, fiber.MethodOptions: {},
}

// New builds the collectors on a registry of their own, next to the ones of the Go runtime and of the process
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		routes:   map[string]struct{}{},
		now:      time.Now,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "http_requests_total", Help: "Requests answered, by method, route and status code.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "http_request_duration_seconds", Help: "Time to answer a request, by method and route.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "http_requests_in_flight", Help: "Requests that are being answered now.",
		}),
		limited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "rate_limited_total", Help: "Requests refused by a rate limiter, by route.",
		}, []string{"route"}),
	}

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.duration, m.inFlight, m.limited,
	)

	return m
}

// Registry is where more collectors are added
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

// RegisterPool adds the statistics of the connection pool of the database
func (m *Metrics) RegisterPool(pool *pgxpool.Pool) {
	gauge := func(name, help string, value func(*pgxpool.Stat) float64) {
		m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Namespace: namespace, Subsystem: "db_pool", Name: name, Help: help},
			func() float64 { return value(pool.Stat()) }))
	}

	counter := func(name, help string, value func(*pgxpool.Stat) float64) {
		m.registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Namespace: namespace, Subsystem: "db_pool", Name: name, Help: help},
			func() float64 { return value(pool.Stat()) }))
	}

	gauge("connections_acquired", "Connections that a query holds now.", func(s *pgxpool.Stat) float64 { return float64(s.AcquiredConns()) })
	gauge("connections_idle", "Connections that wait in the pool.", func(s *pgxpool.Stat) float64 { return float64(s.IdleConns()) })
	gauge("connections_total", "Connections that are open.", func(s *pgxpool.Stat) float64 { return float64(s.TotalConns()) })
	gauge("connections_max", "The most connections that the pool opens.", func(s *pgxpool.Stat) float64 { return float64(s.MaxConns()) })
	counter("acquires_total", "Acquires of a connection.", func(s *pgxpool.Stat) float64 { return float64(s.AcquireCount()) })
	counter("acquires_waited_total", "Acquires that had to wait because every connection was taken.", func(s *pgxpool.Stat) float64 { return float64(s.EmptyAcquireCount()) })
	counter("acquires_canceled_total", "Acquires that gave up before they got a connection.", func(s *pgxpool.Stat) float64 { return float64(s.CanceledAcquireCount()) })
	counter("acquire_wait_seconds_total", "Time that acquires waited for a connection.", func(s *pgxpool.Stat) float64 { return s.AcquireDuration().Seconds() })
}

// AllowRoutes names the paths that may appear as a label, the routes and the groups whose middleware can refuse a request before it
// reaches a route. The middleware of the whole app has the path "/", which says nothing about a request, so it is left out. It is
// called once, after every route is registered.
func (m *Metrics) AllowRoutes(routes []fiber.Route) {
	for _, route := range routes {
		if route.Path == "" || route.Path == "/" {
			continue
		}

		m.routes[route.Path] = struct{}{}
	}
}

// Middleware measures every request that reaches it. It sits inside the access log and answers an error itself, with the error
// handler of the app, so the status it counts is the one the client gets.
func (m *Metrics) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := m.now()

		m.inFlight.Inc()
		defer m.inFlight.Dec()

		err := c.Next()
		if err != nil {
			if handled := c.App().Config().ErrorHandler(c, err); handled != nil {
				_ = c.SendStatus(http.StatusInternalServerError)
			}

			m.countRefusal(c, err)
		}

		m.record(c, c.Response().StatusCode(), start)

		return nil
	}
}

func (m *Metrics) record(c fiber.Ctx, status int, start time.Time) {
	method := "OTHER"
	if _, ok := methods[c.Method()]; ok {
		method = c.Method()
	}

	route := m.routeOf(c)

	m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(method, route).Observe(m.now().Sub(start).Seconds())
}

// routeOf is the path that a route was registered with, so /v1/devices/:id is one series and not one for every id
func (m *Metrics) routeOf(c fiber.Ctx) string {
	route := c.Route()
	if route == nil {
		return Unknown
	}

	if _, ok := m.routes[route.Path]; !ok {
		return Unknown
	}

	return route.Path
}

// countRefusal counts the 429 of a limiter
func (m *Metrics) countRefusal(c fiber.Ctx, err error) {
	var apiErr *api.Error
	if errors.As(err, &apiErr) && apiErr.Code == rateLimitedCode {
		m.limited.WithLabelValues(m.routeOf(c)).Inc()
	}
}
