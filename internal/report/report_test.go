package report_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

func TestTheReportFollowsThePhase(t *testing.T) {
	e := testutil.New(t)

	empty := e.SignUp(t, "kosong@example.com")
	if r := e.Call(t, http.MethodGet, "/v1/reports", "", empty); r.Status != http.StatusOK || r.Body["phase"] != "no-devices" || len(r.Body["periods"].([]any)) != 0 {
		t.Fatalf("no device gave %d %v", r.Status, r.Body)
	}

	g := e.SeedDevice(t, "budi@example.com", "ESP32-AABBCC")
	g.Hours(t, 3*24, 1, 100, 0.1)

	r := e.Call(t, http.MethodGet, "/v1/reports", "", g.Session)
	if r.Body["phase"] != "collecting" || r.Body["neededDays"] != 7.0 || r.Body["collectedDays"] != 3.0 || r.Body["saving"] != nil {
		t.Fatalf("three days gave %v", r.Body)
	}

	if p := e.Call(t, http.MethodGet, "/v1/reports/2026-01/pdf", "", g.Session); p.Status != http.StatusNotFound {
		t.Fatalf("a pdf with no report answered %d", p.Status)
	}
}

func TestTheRunningMonthHasTheThreeSumsTheTopDevicesAndTheBill(t *testing.T) {
	e := testutil.New(t)
	g := e.SeedDevice(t, "budi@example.com", "ESP32-AABBCC")
	g.Hours(t, 8*24, 1, 100, 0.1)

	// the tip waits first, so it is an estimate
	e.Exec(t, "UPDATE businesses SET tips_refreshed_at = NULL")

	r := e.Call(t, http.MethodGet, "/v1/reports", "", g.Session)
	saving, _ := r.Body["saving"].(map[string]any)
	estimate, _ := saving["estimate"].(map[string]any)
	verified, _ := saving["verified"].(map[string]any)
	tariff, _ := r.Body["tariff"].(map[string]any)
	top, _ := r.Body["topDevices"].([]any)

	if r.Status != http.StatusOK || r.Body["phase"] != "ready" || r.Body["through"] == nil || estimate["rp"] != 71000.0 || estimate["count"] != 1.0 ||
		verified["count"] != 0.0 || verified["checkedDays"] != 14.0 || tariff["rpPerKwh"] != 1444.0 || tariff["isDefault"] != true || len(top) != 1 ||
		r.Body["billRp"] == nil {
		t.Fatalf("the report is %v", r.Body)
	}

	periods, _ := r.Body["periods"].([]any)
	if len(periods) == 0 || periods[0] != r.Body["period"] {
		t.Fatalf("the periods are %v", periods)
	}

	// an applied tip moves from the estimate to the applied sum
	list := e.Call(t, http.MethodGet, "/v1/recommendations", "", g.Session)
	id, _ := list.Body["items"].([]any)[0].(map[string]any)["id"].(string)
	e.Call(t, http.MethodPost, "/v1/recommendations/"+id+"/apply", "", g.Session)

	saving, _ = e.Call(t, http.MethodGet, "/v1/reports", "", g.Session).Body["saving"].(map[string]any)
	applied, _ := saving["applied"].(map[string]any)
	estimate, _ = saving["estimate"].(map[string]any)

	if applied["count"] != 1.0 || applied["rp"] != 71000.0 || applied["day"] != 1.0 || applied["neededDays"] != 14.0 || estimate["count"] != 0.0 {
		t.Fatalf("the sums are %v", saving)
	}
}

func TestAMonthWithoutDataIs404AndABadPeriodIs422(t *testing.T) {
	e := testutil.New(t)
	g := e.SeedDevice(t, "budi@example.com", "ESP32-AABBCC")
	g.Hours(t, 8*24, 1, 100, 0.1)

	if r := e.Call(t, http.MethodGet, "/v1/reports?period=2019-01", "", g.Session); r.Status != http.StatusNotFound {
		t.Fatalf("an old month answered %d", r.Status)
	}

	for _, bad := range []string{"2026-13", "x", "2026-1", "2026-01-01"} {
		if r := e.Call(t, http.MethodGet, "/v1/reports?period="+bad, "", g.Session); r.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s answered %d", bad, r.Status)
		}
	}

	if r := e.Call(t, http.MethodGet, "/v1/reports", "", ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("no session answered %d", r.Status)
	}
}

func TestThePdfIsOnePageWithTheNameOfTheMonth(t *testing.T) {
	e := testutil.New(t)
	g := e.SeedDevice(t, "budi@example.com", "ESP32-AABBCC")
	g.Hours(t, 8*24, 1, 100, 0.1)

	period := time.Now().In(time.FixedZone("WIB", 7*3600)).Format("2006-01")

	r := e.Call(t, http.MethodGet, "/v1/reports/"+period+"/pdf", "", g.Session)
	if r.Status != http.StatusOK || r.Header.Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(r.Raw, []byte("%PDF-")) {
		t.Fatalf("the pdf answered %d %q", r.Status, r.Header.Get("Content-Type"))
	}

	disposition := r.Header.Get("Content-Disposition")
	if !strings.HasPrefix(disposition, `attachment; filename="Laporan-`) || !strings.HasSuffix(disposition, `.pdf"`) {
		t.Fatalf("the name is %q", disposition)
	}

	if !strings.Contains(r.Header.Get("Cache-Control"), "no-store") {
		t.Fatalf("the pdf may be cached %q", r.Header.Get("Cache-Control"))
	}

	if bad := e.Call(t, http.MethodGet, "/v1/reports/not-a-month/pdf", "", g.Session); bad.Status != http.StatusNotFound {
		t.Fatalf("a bad month answered %d", bad.Status)
	}
}
