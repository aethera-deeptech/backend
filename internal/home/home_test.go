package home_test

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

func screen(t *testing.T, e *testutil.Env, session string) testutil.Reply {
	t.Helper()

	e.Exec(t, "UPDATE businesses SET tips_refreshed_at = NULL")

	r := e.Call(t, http.MethodGet, "/v1/beranda", "", session)
	if r.Status != http.StatusOK {
		t.Fatalf("beranda answered %d %v", r.Status, r.Body)
	}

	return r
}

func TestTheShapeFollowsThePhase(t *testing.T) {
	e := testutil.New(t)

	empty := e.SignUp(t, "kosong@example.com")
	r := screen(t, e, empty)

	if r.Body["phase"] != "no-devices" || r.Body["businessName"] != "Toko Maju" || r.Body["asOf"] == nil {
		t.Fatalf("no device gave %v", r.Body)
	}

	for _, key := range []string{"statusCounts", "bill", "recommendations", "topDevices", "collectedDays"} {
		if _, ok := r.Body[key]; ok {
			t.Fatalf("a business with no device has %s", key)
		}
	}

	g := e.SeedDevice(t, "budi@example.com", "ESP32-AABBCC")
	g.Hours(t, 3*24, 1, 100, 0.1)

	r = screen(t, e, g.Session)
	top, _ := r.Body["topDevices"].([]any)

	// the top devices count the month so far, so the hours of the last 3 days that fall in an earlier month are left out
	wib, _ := time.LoadLocation("Asia/Jakarta")
	nowHour := time.Now().Truncate(time.Hour)
	monthStart := time.Date(nowHour.In(wib).Year(), nowHour.In(wib).Month(), 1, 0, 0, 0, 0, wib)
	inMonth := 0

	for h := 1; h <= 3*24; h++ {
		if !nowHour.Add(-time.Duration(h) * time.Hour).Before(monthStart) {
			inMonth++
		}
	}

	wantTop := 1
	if inMonth == 0 {
		wantTop = 0
	}

	if r.Body["phase"] != "collecting" || r.Body["neededDays"] != 7.0 || r.Body["collectedDays"] != 3.0 || len(top) != wantTop {
		t.Fatalf("three days gave %v", r.Body)
	}

	if _, ok := r.Body["bill"]; ok {
		t.Fatal("a business that collects data has a bill")
	}

	if wantTop == 1 {
		if first, _ := top[0].(map[string]any); first["name"] != "Mesin jahit 1" || first["kwh"] != math.Round(float64(inMonth)*0.1*10)/10 {
			t.Fatalf("the top device is %v", first)
		}
	}
}

func TestAReadyBusinessGetsTheStatusesTheTipsAndTheBill(t *testing.T) {
	e := testutil.New(t)
	g := e.SeedDevice(t, "budi@example.com", "ESP32-AABBCC")
	g.Hours(t, 8*24, 1, 100, 0.1)

	r := screen(t, e, g.Session)

	counts, _ := r.Body["statusCounts"].(map[string]any)
	tipList, _ := r.Body["recommendations"].([]any)
	bill, _ := r.Body["bill"].(map[string]any)
	daily, _ := bill["dailyTotalRp"].([]any)

	if r.Body["phase"] != "ready" || counts["offline"] != 1.0 || counts["nyala"] != 0.0 || r.Body["savingPerMonthRp"] != 71000.0 || len(tipList) != 1 {
		t.Fatalf("the screen is %v", r.Body)
	}

	if tip, _ := tipList[0].(map[string]any); tip["deviceName"] != "Mesin jahit 1" || tip["savingPerMonthRp"] != 71000.0 {
		t.Fatalf("the tip is %v", tip)
	}

	// one total for each day of the month so far, and the totals only grow
	if want := time.Now().In(time.FixedZone("WIB", 7*3600)).Day(); len(daily) != want {
		t.Fatalf("the bill has %d days, want %d", len(daily), want)
	}

	last := 0.0

	for _, v := range daily {
		if v.(float64) < last {
			t.Fatalf("the totals fall %v", daily)
		}

		last = v.(float64)
	}

	projected, _ := bill["projectedTotalRp"].(float64)
	potential, _ := bill["potentialTotalRp"].(float64)

	if projected < last || potential != max(0, projected-71000) {
		t.Fatalf("the bill is %v", bill)
	}

	if _, ok := r.Body["energyPerPiece"]; ok {
		t.Fatal("there is no verified tip yet")
	}

	// a tip that is applied leaves the home screen
	id, _ := tipList[0].(map[string]any)["id"].(string)
	if a := e.Call(t, http.MethodPost, "/v1/recommendations/"+id+"/apply", "", g.Session); a.Status != http.StatusOK {
		t.Fatalf("apply answered %d", a.Status)
	}

	r = screen(t, e, g.Session)

	left, ok := r.Body["recommendations"].([]any)
	if !ok || len(left) != 0 || r.Body["savingPerMonthRp"] != 0.0 {
		t.Fatalf("after the decision the screen is %v", r.Body)
	}
}
