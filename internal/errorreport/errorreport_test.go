package errorreport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

const testDSN = "https://public@o0.ingest.example.invalid/1"

// recorder is a transport that keeps the events instead of sending them
type recorder struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (r *recorder) Flush(time.Duration) bool              { return true }
func (r *recorder) FlushWithContext(context.Context) bool { return true }
func (r *recorder) Configure(sentry.ClientOptions)        {}
func (r *recorder) Close()                                {}

func (r *recorder) SendEvent(event *sentry.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, event)
}

func (r *recorder) all() []*sentry.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]*sentry.Event(nil), r.events...)
}

// start turns the reports on with a recorder and a clock that the test moves. The reports are global, so the tests of this
// package do not run in parallel.
func start(t *testing.T, perMinute int) (*recorder, *time.Time) {
	t.Helper()

	rec := &recorder{}
	clock := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	stop := Start(Config{
		DSN: testDSN, Environment: "test", ServerName: "sora", Release: "1.2.3", EventsPerMinute: perMinute,
		Transport: rec, Now: func() time.Time { return clock },
	})
	t.Cleanup(stop)

	return rec, &clock
}

// textsOf joins every text of an event that could hold a secret
func textsOf(event *sentry.Event) string {
	parts := []string{event.Message, event.Transaction}

	for _, exception := range event.Exception {
		parts = append(parts, exception.Type, exception.Value)
	}

	for _, value := range event.Tags {
		parts = append(parts, value)
	}

	return strings.Join(parts, "\n")
}

func TestTheReportsAreOffWithoutADSN(t *testing.T) {
	stop := Start(Config{EventsPerMinute: 30})
	defer stop()

	if active.Load() != nil {
		t.Fatal("the reports are on without a DSN")
	}

	// nothing is sent and nothing panics
	Report(errors.New("boom"), Info{Status: 500})
	Panic("boom", Info{})
}

func TestADSNThatCannotBeReadLeavesTheReportsOff(t *testing.T) {
	stop := Start(Config{DSN: "this is not a dsn", EventsPerMinute: 30})
	defer stop()

	if active.Load() != nil {
		t.Fatal("a bad setting must not keep the API from starting")
	}
}

func TestAReportCarriesTheNamesAndIdsAndNothingOfTheRequest(t *testing.T) {
	rec, _ := start(t, 30)

	Report(errors.New("insert failed"), Info{
		Route: "/v1/devices/:id", Status: 500, RequestID: "0b8f6a52-3c4d-4e5f-8a6b-7c8d9e0f1a2b", UserID: "7b1d2c3e-4f56-4a78-9b01-2c3d4e5f6a7b",
	})

	events := rec.all()
	if len(events) != 1 {
		t.Fatalf("%d events were sent", len(events))
	}

	e := events[0]

	if e.Release != "1.2.3" || e.Environment != "test" || e.ServerName != "sora" || e.Tags["route"] != "/v1/devices/:id" || e.Tags["status"] != "500" {
		t.Fatalf("the event is %+v", e)
	}

	// the request id is not scrubbed, it is how the log line is found
	if e.Tags["request_id"] != "0b8f6a52-3c4d-4e5f-8a6b-7c8d9e0f1a2b" || !reflect.DeepEqual(e.User, sentry.User{ID: "7b1d2c3e-4f56-4a78-9b01-2c3d4e5f6a7b"}) {
		t.Fatalf("the tags or the user are %v %+v", e.Tags, e.User)
	}

	if e.Request != nil || len(e.Breadcrumbs) != 0 || len(e.Exception) == 0 {
		t.Fatalf("the event holds the request %v, breadcrumbs %v or no exception", e.Request, e.Breadcrumbs)
	}
}

func TestAReportHidesEmailsTokensAndLongNumbers(t *testing.T) {
	rec, _ := start(t, 30)

	secrets := []string{
		"budi.santoso@example.com",
		// built here so that no token stays in the source, the secret scan reads the files and the history
		strings.Join([]string{"eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiIxMjM0In0", "c2lnbmF0dXJl"}, "."),
		"abcDEF123456abcDEF123456abcDEF123456",
		"1234567890123",
	}

	Report(
		errors.New("ERROR: duplicate key value violates unique constraint, Key (email)=("+secrets[0]+"), Bearer "+secrets[1]+
			", refresh "+secrets[2]+", phone "+secrets[3]),
		Info{Route: "/v1/auth/register", Status: 500},
	)

	events := rec.all()
	if len(events) != 1 {
		t.Fatalf("%d events were sent", len(events))
	}

	text := textsOf(events[0])

	for _, secret := range secrets {
		if strings.Contains(text, secret) {
			t.Fatalf("the secret %q left in %q", secret, text)
		}
	}

	for _, want := range []string{"[email]", "[token]", "[secret]", "[number]", "duplicate key value violates unique constraint"} {
		if !strings.Contains(text, want) {
			t.Fatalf("%q is missing from %q", want, text)
		}
	}
}

func TestAPanicIsReportedWithTheSamePrivacy(t *testing.T) {
	rec, _ := start(t, 30)

	Panic("nil pointer for budi.santoso@example.com", Info{Route: "/v1/beranda", Status: 500, RequestID: "req-1"})

	events := rec.all()
	if len(events) != 1 {
		t.Fatalf("%d events were sent", len(events))
	}

	e := events[0]
	text := textsOf(e) + e.Message

	if strings.Contains(text, "budi.santoso@example.com") || !strings.Contains(text, "[email]") {
		t.Fatalf("the text is %q", text)
	}

	if len(e.Threads) == 0 && (len(e.Exception) == 0 || e.Exception[0].Stacktrace == nil) {
		t.Fatal("a panic report holds the stack")
	}

	if e.Tags["panic"] != "true" || e.Tags["route"] != "/v1/beranda" {
		t.Fatalf("the tags are %v", e.Tags)
	}
}

func TestTheCapDropsTheEventsOverTheLimitAndStartsAgainEachMinute(t *testing.T) {
	rec, clock := start(t, 3)

	for range 5 {
		Report(errors.New("again"), Info{Status: 500})
	}

	if n := len(rec.all()); n != 3 {
		t.Fatalf("%d of 5 passed a cap of 3", n)
	}

	*clock = clock.Add(30 * time.Second)

	Report(errors.New("still the same minute"), Info{Status: 500})

	if n := len(rec.all()); n != 3 {
		t.Fatalf("%d events after 30 seconds", n)
	}

	*clock = clock.Add(31 * time.Second)

	Report(errors.New("a new minute"), Info{Status: 500})
	Report(errors.New("a new minute"), Info{Status: 500})

	if n := len(rec.all()); n != 5 {
		t.Fatalf("%d events after a new minute", n)
	}
}

func TestScrubTextCasesOneByOne(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"an email":            {"a.b+c@mail.example.co.id failed", "[email] failed"},
		"a jwt":               {"token " + strings.Join([]string{"eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiIxIn0", "sig"}, ".") + " end", "token [token] end"},
		"a bearer header":     {"Authorization: Bearer abc.DEF-123_x end", "Authorization: Bearer [token] end"},
		"a long key":          {"key 0123456789abcdef0123456789abcdef0123456789abcdef end", "key [secret] end"},
		"a phone number":      {"phone 628123456789", "phone [number]"},
		"an ipv4 and a port":  {"dial 172.18.0.3:5432 refused", "dial [ip] refused"},
		"an ipv6":             {"from 2001:db8::1 failed", "from 2001:db8::1 failed"},
		"a long ipv6":         {"from 2001:db8:1:2:3:4:5:6 failed", "from [ip] failed"},
		"a short number":      {"status 500 after 1234 ms", "status 500 after 1234 ms"},
		"a uuid stays":        {"row 0b8f6a52-3c4d-4e5f-8a6b-7c8d9e0f1a2b", "row 0b8f6a52-3c4d-4e5f-8a6b-7c8d9e0f1a2b"},
		"a text without data": {"context deadline exceeded", "context deadline exceeded"},
	}

	for name, test := range tests {
		if got := scrubText(test.in); got != test.want {
			t.Errorf("%s gave %q, want %q", name, got, test.want)
		}
	}
}

func TestScrubEventDropsTheRequestTheBreadcrumbsAndTheContactsOfTheUser(t *testing.T) {
	event := &sentry.Event{
		Message:     "failed for budi@example.com",
		Request:     &sentry.Request{URL: "https://api.example.com/v1/x?token=abc", Cookies: "session=abc"},
		Breadcrumbs: []*sentry.Breadcrumb{{Message: "log line"}},
		User:        sentry.User{ID: "u1", Email: "budi@example.com", IPAddress: "10.0.0.1", Username: "budi"},
		Tags:        map[string]string{"route": "/x", "other": "budi@example.com"},
	}

	scrubEvent(event)

	if event.Request != nil || len(event.Breadcrumbs) != 0 || !reflect.DeepEqual(event.User, sentry.User{ID: "u1"}) {
		t.Fatalf("the event still holds %+v", event)
	}

	if event.Message != "failed for [email]" || event.Tags["route"] != "/x" || event.Tags["other"] != "[email]" {
		t.Fatalf("the texts are %q %v", event.Message, event.Tags)
	}
}
