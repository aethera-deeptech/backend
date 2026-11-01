package tips

import (
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/business"
)

func TestIsWorkHour(t *testing.T) {
	jakarta := time.FixedZone("WIB", 7*3600)
	// 2026-11-02 is a Monday, 2026-11-07 a Saturday and 2026-11-08 a Sunday
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, jakarta) }
	sat := business.WorkHours{Days: "mon-sat", Start: "08:00", End: "17:00"}

	tests := map[string]struct {
		local time.Time
		wh    business.WorkHours
		want  bool
	}{
		"monday morning":          {at(2, 9, 0), sat, true},
		"first hour":              {at(2, 8, 0), sat, true},
		"last hour":               {at(2, 16, 0), sat, true},
		"after the end":           {at(2, 17, 0), sat, false},
		"night":                   {at(2, 2, 0), sat, false},
		"before the start":        {at(2, 7, 0), sat, false},
		"saturday in mon-sat":     {at(7, 10, 0), sat, true},
		"sunday in mon-sat":       {at(8, 10, 0), sat, false},
		"saturday in mon-fri":     {at(7, 10, 0), business.WorkHours{Days: "mon-fri", Start: "08:00", End: "17:00"}, false},
		"sunday when daily":       {at(8, 10, 0), business.WorkHours{Days: "daily", Start: "08:00", End: "17:00"}, true},
		"an hour that is touched": {at(2, 7, 0), business.WorkHours{Days: "daily", Start: "07:30", End: "16:30"}, true},
		"a broken setting":        {at(2, 3, 0), business.WorkHours{Days: "daily", Start: "x", End: "y"}, true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := IsWorkHour(tt.local, tt.wh); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoundRpUsesThousands(t *testing.T) {
	// the sums of the design, 81,7 and 62,4 and 72 and 31,9 kWh at the default tariff
	for kwh, want := range map[float64]int{81.7: 118000, 62.4: 90000, 72: 104000, 31.9: 46000, 0.1: 0} {
		if got := RoundRp(kwh, 1444); got != want {
			t.Fatalf("%v kWh is %d, want %d", kwh, got, want)
		}
	}
}

func TestNumbersAreWrittenTheIndonesianWay(t *testing.T) {
	for _, tt := range []struct {
		v        float64
		decimals int
		want     string
	}{{81.7, 1, "81,7"}, {1444, 0, "1.444"}, {1234567.891, 2, "1.234.567,89"}, {-3.26, 1, "-3,3"}, {0, 2, "0,00"}, {999, 0, "999"}} {
		if got := num(tt.v, tt.decimals); got != tt.want {
			t.Fatalf("num(%v, %d) = %q, want %q", tt.v, tt.decimals, got, tt.want)
		}
	}

	if got := rp(118000); got != "Rp118.000" {
		t.Fatalf("rp = %q", got)
	}
}

func rows(days int, watts, energy float64) []hourRow {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	out := make([]hourRow, 0, days*24)

	for i := range days * 24 {
		out = append(out, hourRow{hour: start.Add(time.Duration(i) * time.Hour), watts: watts, energy: energy})
	}

	return out
}

func TestClassifyPicksTheKindFromTheOffHoursPower(t *testing.T) {
	wh := business.WorkHours{Days: "mon-sat", Start: "08:00", End: "17:00"}
	utc := time.UTC

	if kind, kwh := classify(rows(7, 100, 0.1), utc, wh, 7); kind != KindAfterHours || kwh <= 0 {
		t.Fatalf("a device that runs gives %q %v", kind, kwh)
	}

	if kind, _ := classify(rows(7, 4, 0.004), utc, wh, 7); kind != KindStandby {
		t.Fatalf("a device in standby gives %q", kind)
	}

	if kind, _ := classify(rows(7, 0.3, 0.0003), utc, wh, 7); kind != "" {
		t.Fatalf("a device that is off gives %q", kind)
	}

	if kind, _ := classify(rows(1, 100, 0.1)[:10], utc, wh, 7); kind != "" {
		t.Fatalf("too few hours give %q", kind)
	}

	// 30 days times the energy of the hours outside the work time, over the 7 days
	_, kwh := classify(rows(7, 100, 0.1), utc, wh, 7)
	if kwh < 40 || kwh > 60 {
		t.Fatalf("the month saving is %v", kwh)
	}
}

func TestTextsFollowTheDeviceAndTheState(t *testing.T) {
	f := facts{kind: KindAfterHours, deviceKind: "plug", name: "Mesin jahit 3", status: StatusPending, kwhMonth: 81.7, tariff: 1444, tariffDefault: true}

	if f.title() != "Matikan Mesin jahit 3 di luar jam kerja" {
		t.Fatalf("title %q", f.title())
	}

	if got := f.whatToDo(); got != "Matikan Mesin jahit 3 dari colokan setelah jam kerja. Anda yang memutuskan. Sistem hanya menyarankan dan tidak mematikan alat." {
		t.Fatalf("what to do %q", got)
	}

	if got := f.how(); len(got) != 2 || got[1] != "Hemat per bulan = energi yang dihemat dalam kWh, dikali tarif listrik per kWh. Hitungannya, 81,7 kWh dikali tarif bawaan Rp1.444 per kWh sama dengan sekitar Rp118.000 per bulan." {
		t.Fatalf("how %q", got)
	}

	f.deviceKind, f.status, f.tariffDefault = "clamp", StatusDeferred, false

	if got := f.whatToDo(); got[:26] != "Anda menunda saran ini dan" || f.tariffLabel() != "tarif Anda" {
		t.Fatalf("deferred text %q", got)
	}

	f.kind = KindStandby
	if f.title() != "Kurangi daya siaga Mesin jahit 3" {
		t.Fatalf("standby title %q", f.title())
	}

	f.status, f.hasResult, f.beforeKwh, f.afterKwh, f.percent, f.measuredKwh = StatusVerified, true, 0.41, 0.37, 9.8, 44.3
	if got := f.how(); len(got) != 3 {
		t.Fatalf("a verified tip has %d paragraphs", len(got))
	}
}
