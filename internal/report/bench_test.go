package report

import (
	"bytes"
	"testing"
	"time"
)

// BenchmarkRender is the cost of one PDF of the report, which a person can ask for as often as they like
func BenchmarkRender(b *testing.B) {
	r := sample()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	for b.Loop() {
		var buf bytes.Buffer
		if err := render(&buf, r, start); err != nil {
			b.Fatal(err)
		}
	}
}
