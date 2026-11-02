package tips

import (
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/business"
)

// BenchmarkClassify is the work of the refresh for one device, which reads 7 days of hours
func BenchmarkClassify(b *testing.B) {
	loc := time.FixedZone("WIB", 7*3600)
	wh := business.WorkHours{Days: "mon-sat", Start: "08:00", End: "17:00"}
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	rows := make([]hourRow, 0, 7*24)

	for h := range 7 * 24 {
		rows = append(rows, hourRow{hour: start.Add(time.Duration(h) * time.Hour), energy: 0.01 + float64(h%5)*0.002, watts: 9 + float64(h%7)})
	}

	for b.Loop() {
		classify(rows, loc, wh, BaselineDays)
	}
}
