package errorreport

import "testing"

// BenchmarkScrubText is the cost of the scrub of one error text on the path of a 5xx
func BenchmarkScrubText(b *testing.B) {
	text := "request to budi@example.com failed with Bearer abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG from 192.168.0.1 and 2001:db8::1, " +
		"key 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef, phone 081234567890, " +
		"after reading a plain text that holds no secret at all, which is what most errors look like"

	for b.Loop() {
		scrubText(text)
	}
}
