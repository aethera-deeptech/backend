package errorreport

import (
	"testing"
	"unicode/utf8"
)

// FuzzScrubText feeds the scrub with any text. It must never panic and it must keep a text that is valid UTF-8 valid, because the
// text goes into an event that the Sentry client encodes.
func FuzzScrubText(f *testing.F) {
	for _, seed := range []string{
		"",
		"plain text",
		"sign in failed for budi@example.com",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",
		"key 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef and 081234567890",
		"2001:db8::1 and 192.168.0.1 and ::ffff:10.0.0.1",
		"\xff\xfe broken \xc3",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, text string) {
		out := scrubText(text)

		if utf8.ValidString(text) && !utf8.ValidString(out) {
			t.Fatalf("the scrub of %q is not valid UTF-8", text)
		}
	})
}
