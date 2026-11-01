package errorreport

import (
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
)

// The patterns that are replaced in every text of an event, in this order. A database error can carry the values of a row, such
// as `Key (email)=(a@b.c) already exists`, so an email, a token and a long number such as a phone number must not leave. The patterns are a best effort and the tests hold one case for each of them.
var scrubbers = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`), "[token]"},
	{regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`), "Bearer [token]"},
	{regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`), "[email]"},
	// an IPv4 address with an optional port, which a net error can hold for a client or for the database
	{regexp.MustCompile(`\b[0-9]{1,3}(?:\.[0-9]{1,3}){3}(?::[0-9]{1,5})?\b`), "[ip]"},
	// an IPv6 address
	{regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){3,7}[0-9a-f]{1,4}\b`), "[ip]"},
	// a hash, a refresh token or a key, a run of 32 or more letters and digits (the groups of a UUID are shorter)
	{regexp.MustCompile(`[A-Za-z0-9_]{32,}`), "[secret]"},
	// a phone number or any other long number
	{regexp.MustCompile(`[0-9]{5,}`), "[number]"},
}

// scrubText replaces what must not leave a text
func scrubText(text string) string {
	for _, scrubber := range scrubbers {
		text = scrubber.pattern.ReplaceAllString(text, scrubber.replacement)
	}

	return text
}

// tagsKept are the tags that hold an id or a name that the code chose, so they are not scrubbed. A request id is a UUID, and
// the scrub would otherwise cut the digits out of it.
var tagsKept = map[string]bool{"request_id": true, "route": true, "status": true, "panic": true}

// scrubEvent removes what an event must not hold and scrubs every text of it
func scrubEvent(event *sentry.Event) {
	event.Request = nil
	event.Breadcrumbs = nil
	event.Attachments = nil

	event.Message = scrubText(event.Message)
	event.Transaction = scrubText(event.Transaction)

	for i := range event.Exception {
		event.Exception[i].Type = scrubText(event.Exception[i].Type)
		event.Exception[i].Value = scrubText(event.Exception[i].Value)
	}

	for key, value := range event.Tags {
		if !tagsKept[key] {
			event.Tags[key] = scrubText(value)
		}
	}

	// only the id of the account may stay
	event.User = sentry.User{ID: event.User.ID}
}

// minuteCap lets at most max events through in each minute and counts the rest. A line is logged when a minute starts to drop
// events and another with the count when that minute is over, so a storm is visible without a line for every event.
type minuteCap struct {
	mu      sync.Mutex
	max     int
	now     func() time.Time
	started time.Time
	sent    int
	dropped int
}

func newCap(max int, now func() time.Time) *minuteCap {
	return &minuteCap{max: max, now: now}
}

func (c *minuteCap) allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	moment := c.now()

	if c.started.IsZero() || moment.Sub(c.started) >= time.Minute {
		if c.dropped > 0 {
			slog.Warn("error reports were dropped by the cap in the last minute", slog.Int("dropped", c.dropped), slog.Int("cap_per_minute", c.max))
		}

		c.started, c.sent, c.dropped = moment, 0, 0
	}

	if c.sent >= c.max {
		c.dropped++

		if c.dropped == 1 {
			slog.Warn("error reports are being dropped by the cap", slog.Int("cap_per_minute", c.max))
		}

		return false
	}

	c.sent++

	return true
}
