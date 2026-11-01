// Package errorreport sends the failures that the API did not choose, a 5xx and a panic, to Sentry. What leaves is decided here.
// An event carries the error, the stack, the release, the environment and a few names and ids,
// and never the request. Every text is scrubbed, and a cap keeps a crash loop from using up the quota.
package errorreport

import (
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
)

// Config holds the settings of the reports. An empty DSN switches the reports off, and then no client is made at all.
type Config struct {
	DSN             string
	Environment     string
	ServerName      string
	Release         string
	EventsPerMinute int

	// Transport replaces the connection to Sentry, so a test can see what would leave
	Transport sentry.Transport
	// Now replaces the clock of the cap, for tests
	Now func() time.Time
}

// Info is what an event may carry besides the error. It holds names and ids and nothing of the request.
type Info struct {
	// Route is the pattern of the route, such as /v1/devices/:id, and never the address that was called
	Route     string
	Status    int
	RequestID string
	// UserID is the id of the account, and empty for a request that was not signed in
	UserID string
}

// flushTimeout is how long the shutdown waits for the queue, so the errors of a rolling deploy are not lost
const flushTimeout = 2 * time.Second

type reporter struct {
	client *sentry.Client
	limit  *minuteCap
}

var active atomic.Pointer[reporter]

// Start makes the client and turns the reports on. The returned function sends what is still queued and stops them, and it is
// safe to call when the reports are off. A DSN that cannot be read is logged without its value and leaves the reports off,
// because an error reporter must never keep the API from starting.
func Start(cfg Config) func() {
	if cfg.DSN == "" {
		return func() {}
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	limit := newCap(cfg.EventsPerMinute, now)

	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:         cfg.DSN,
		Release:     cfg.Release,
		Environment: cfg.Environment,
		ServerName:  cfg.ServerName,
		Transport:   cfg.Transport,
		// the cap of Report and Panic is the limit, and an event that passes it is always sent
		SampleRate: 1,
		// nothing of a request is collected, and no breadcrumbs, which can hold log lines. The code never hands a request to the
		// SDK, so this is the second lock after the scrub of BeforeSend, and only the user that the code sets stays.
		DataCollection: &sentry.DataCollection{
			UserInfo: sentry.Set(false),
			Cookies:  &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
			HTTPHeaders: &sentry.HeaderCollectionConfig{
				Request:  &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
				Response: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
			},
			HTTPBodies:  []sentry.BodyType{},
			QueryParams: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
		},
		MaxBreadcrumbs: -1,
		EnableTracing:  false,
		// a panic with a string becomes a message event, which has a stack only when this is on
		AttachStacktrace: true,
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			scrubEvent(event)

			return event
		},
	})
	if err != nil {
		slog.Error("error reports are off because the Sentry settings are not valid", slog.String("reason", scrubText(err.Error())))

		return func() {}
	}

	active.Store(&reporter{client: client, limit: limit})
	slog.Info("error reports go to Sentry", slog.String("environment", cfg.Environment))

	return func() {
		active.Store(nil)
		client.Flush(flushTimeout)
		client.Close()
	}
}

// Report sends an error that became a failure of the server. It does nothing when the reports are off.
func Report(err error, info Info) {
	current := active.Load()
	if current == nil || err == nil || !current.limit.allow() {
		return
	}

	defer guard()

	current.client.CaptureException(err, nil, scopeOf(info))
}

// Panic sends a panic that a handler raised. It does nothing when the reports are off.
func Panic(recovered any, info Info) {
	current := active.Load()
	if current == nil || !current.limit.allow() {
		return
	}

	defer guard()

	scope := scopeOf(info)
	scope.SetTag("panic", "true")

	current.client.Recover(recovered, nil, scope)
}

// guard keeps a failure inside the reporter from leaving it. The reports run on the goroutine of a request and in the recovery of a
// panic, and a panic there would end the process, which is the opposite of what a reporter is for. The log line holds no value.
func guard() {
	if recover() != nil {
		slog.Error("the error report failed")
	}
}

func scopeOf(info Info) *sentry.Scope {
	scope := sentry.NewScope()
	scope.SetLevel(sentry.LevelError)

	if info.Route != "" {
		scope.SetTag("route", info.Route)
	}

	if info.Status != 0 {
		scope.SetTag("status", fmt.Sprint(info.Status))
	}

	if info.RequestID != "" {
		scope.SetTag("request_id", info.RequestID)
	}

	if info.UserID != "" {
		scope.SetUser(sentry.User{ID: info.UserID})
	}

	return scope
}
