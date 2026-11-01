package report

import (
	"bytes"
	"regexp"
	"testing"
	"time"
)

var pageObject = regexp.MustCompile(`/Type /Page[^s]`)

func sample() Report {
	now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	top := []Top{{"Setrika uap", 160}, {"Mesin jahit 1", 142}, {"Kompresor", 120}, {"Mesin jahit 3", 135}}

	return Report{
		Phase: "ready", Period: "2026-10", Periods: []string{"2026-10"}, Through: &now, Tariff: &Tariff{RpPerKwh: 1444, IsDefault: true},
		Saving: &Saving{
			Verified: Verified{Rp: 64000, Count: 1, CheckedDays: 14}, Applied: Applied{Rp: 46000, Count: 1, Day: 9, NeededDays: 14},
			Estimate: Estimate{Rp: 312000, Count: 3},
		},
		TopDevices: &top, BillRp: new(1965000), EnergyPerPiece: &PerPiece{Status: "verified", BeforeKwh: 0.41, AfterKwh: 0.37},
		businessName: "Konveksi Maju Jaya", unit: "potong",
	}
}

func TestTheReportIsOneA4Page(t *testing.T) {
	var buf bytes.Buffer
	if err := render(&buf, sample(), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	b := buf.Bytes()
	if !bytes.HasPrefix(b, []byte("%PDF-")) || len(b) < 1000 {
		t.Fatalf("the output is %d bytes and starts with %q", len(b), b[:min(8, len(b))])
	}

	if n := len(pageObject.FindAll(b, -1)); n != 1 {
		t.Fatalf("the file has %d pages", n)
	}

	if !bytes.Contains(b, []byte("/MediaBox [0 0 595.28 841.89]")) {
		t.Fatal("the page is not A4")
	}
}

func TestThePdfSurvivesOddInput(t *testing.T) {
	r := sample()
	r.businessName = "Toko \U0001F600 กข dan nama yang sangat panjang sekali sekali sekali sekali sekali sekali sekali sekali"
	r.Through, r.EnergyPerPiece = nil, nil
	r.Tariff = &Tariff{RpPerKwh: 1500}
	empty := []Top{}
	r.TopDevices = &empty

	var buf bytes.Buffer
	if err := render(&buf, r, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	if n := len(pageObject.FindAll(buf.Bytes(), -1)); n != 1 {
		t.Fatalf("the file has %d pages", n)
	}
}

func TestLabels(t *testing.T) {
	if got := monthLabel(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)); got != "Oktober 2026" {
		t.Fatalf("month label %q", got)
	}

	if got := throughLabel(time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)); got != "29 Okt" {
		t.Fatalf("through label %q", got)
	}

	if got := countLine(0, "none", "%d saran"); got != "none" {
		t.Fatalf("empty line %q", got)
	}

	if got := countLine(3, "none", "%d saran"); got != "3 saran" {
		t.Fatalf("count line %q", got)
	}
}
