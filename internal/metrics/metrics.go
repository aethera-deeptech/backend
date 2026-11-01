// Package metrics counts what the API does and serves it in the Prometheus text format on a listener of its own. The listener is never
// the public one, so what it shows (route names, error rates, pool sizes) cannot be read from the internet, and a scraper reaches it
// only through the private network of the deployment.
package metrics

import (
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

// RegisterPool adds the statistics of the connection pool of the database. One scrape reads the statistics once, so the numbers of a
// scrape belong together.
func (m *Metrics) RegisterPool(pool *pgxpool.Pool) {
	m.registry.MustRegister(newPoolCollector(pool))
}

type poolCollector struct {
	pool *pgxpool.Pool
	descs
}

type descs struct {
	acquired, idle, total, max              *prometheus.Desc
	acquires, waited, canceled, waitSeconds *prometheus.Desc
}

func newPoolCollector(pool *pgxpool.Pool) *poolCollector {
	d := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "db_pool", name), help, nil, nil)
	}

	return &poolCollector{pool: pool, descs: descs{
		acquired:    d("connections_acquired", "Connections that a query holds now."),
		idle:        d("connections_idle", "Connections that wait in the pool."),
		total:       d("connections_total", "Connections that are open."),
		max:         d("connections_max", "The most connections that the pool opens."),
		acquires:    d("acquires_total", "Acquires of a connection."),
		waited:      d("acquires_waited_total", "Acquires that had to wait because every connection was taken."),
		canceled:    d("acquires_canceled_total", "Acquires that gave up before they got a connection."),
		waitSeconds: d("acquire_wait_seconds_total", "Time that acquires waited for a connection."),
	}}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.acquired, c.idle, c.total, c.max, c.acquires, c.waited, c.canceled, c.waitSeconds} {
		ch <- d
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()

	gauge := func(d *prometheus.Desc, v float64) { ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v) }
	counter := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v)
	}

	gauge(c.acquired, float64(s.AcquiredConns()))
	gauge(c.idle, float64(s.IdleConns()))
	gauge(c.total, float64(s.TotalConns()))
	gauge(c.max, float64(s.MaxConns()))
	counter(c.acquires, float64(s.AcquireCount()))
	counter(c.waited, float64(s.EmptyAcquireCount()))
	counter(c.canceled, float64(s.CanceledAcquireCount()))
	counter(c.waitSeconds, s.EmptyAcquireWaitTime().Seconds())
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

// Middleware measures every request that reaches it. It sits inside the access log and outside the recovery, and it returns the error
// that it saw, so the access log keeps the code of the error.
func (m *Metrics) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := m.now()

		m.inFlight.Inc()
		defer m.inFlight.Dec()

		err := c.Next()

		// the error goes on to the access log and then to the error handler, once, so this middleware reads the status that the handler
		// will give and changes nothing
		status := c.Response().StatusCode()
		if err != nil {
			status = api.StatusOf(err)
		}

		m.record(c, status, start)

		if status == http.StatusTooManyRequests {
			m.limited.WithLabelValues(m.routeOf(c)).Inc()
		}

		return err
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
