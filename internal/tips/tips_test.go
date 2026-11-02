package tips_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

type rig struct {
	*testutil.Seeded
	e *testutil.Env
}

func seed(t *testing.T, e *testutil.Env, email, hardware string) *rig {
	t.Helper()

	return &rig{Seeded: e.SeedDevice(t, email, hardware), e: e}
}

func (g *rig) hours(t *testing.T, fromAgo, toAgo int, watts, kwh float64) {
	t.Helper()

	g.Hours(t, fromAgo, toAgo, watts, kwh)
}

func (g *rig) list(t *testing.T) testutil.Reply {
	t.Helper()

	g.e.Exec(t, "UPDATE businesses SET tips_refreshed_at = NULL")

	return g.e.Call(t, http.MethodGet, "/v1/recommendations", "", g.Session)
}

func (g *rig) decide(t *testing.T, id, action string) testutil.Reply {
	t.Helper()

	return g.e.Call(t, http.MethodPost, "/v1/recommendations/"+id+"/"+action, "", g.Session)
}

func TestTheFirstTipComesAfterSevenDaysOfData(t *testing.T) {
	e := testutil.New(t)

	empty := e.SignUp(t, "kosong@example.com")
	if r := e.Call(t, http.MethodGet, "/v1/recommendations", "", empty); r.Body["phase"] != "no-devices" || r.Body["items"] != nil {
		t.Fatalf("a business with no device got %v", r.Body)
	}

	g := seed(t, e, "budi@example.com", "ESP32-AABBCC")
	g.hours(t, 3*24, 1, 100, 0.1)

	r := g.list(t)
	if r.Body["phase"] != "collecting" || r.Body["neededDays"] != 7.0 || r.Body["items"] != nil || r.Body["businessName"] != "Toko Maju" {
		t.Fatalf("three days gave %v", r.Body)
	}

	g.hours(t, 8*24, 3*24, 100, 0.1)

	r = g.list(t)

	items, _ := r.Body["items"].([]any)
	if r.Body["phase"] != "ready" || len(items) != 1 {
		t.Fatalf("eight days gave %v", r.Body)
	}

	item, _ := items[0].(map[string]any)

	// 114 hours outside the work time in a week, 0,1 kWh each, for 30 days at the default tariff of 1444
	if item["title"] != "Matikan Mesin jahit 1 di luar jam kerja" || item["status"] != "pending" || item["savingPerMonthRp"] != 71000.0 {
		t.Fatalf("the tip is %v", item)
	}
}

func TestTheOwnerDecidesAndTheStatesFollowTheRules(t *testing.T) {
	e := testutil.New(t)
	g := seed(t, e, "budi@example.com", "ESP32-AABBCC")
	other := seed(t, e, "sari@example.com", "ESP32-DDEEFF")
	g.hours(t, 8*24, 1, 100, 0.1)

	items, _ := g.list(t).Body["items"].([]any)
	id, _ := items[0].(map[string]any)["id"].(string)

	d := e.Call(t, http.MethodGet, "/v1/recommendations/"+id, "", g.Session)
	how, _ := d.Body["howCalculated"].([]any)

	if d.Status != http.StatusOK || d.Body["tariffIsDefault"] != true || d.Body["deviceName"] != "Mesin jahit 1" || len(how) != 2 ||
		!strings.Contains(how[1].(string), "tarif bawaan Rp1.444 per kWh") {
		t.Fatalf("the detail is %v", d.Body)
	}

	// the states, one step at a time
	for _, step := range []struct {
		action string
		status int
		state  string
	}{
		{"cancel", http.StatusConflict, ""},
		{"defer", http.StatusOK, "deferred"},
		{"defer", http.StatusConflict, ""},
		{"apply", http.StatusOK, "applied"},
		{"apply", http.StatusConflict, ""},
		{"reject", http.StatusConflict, ""},
		{"cancel", http.StatusOK, "pending"},
		{"reject", http.StatusOK, "rejected"},
		{"apply", http.StatusConflict, ""},
	} {
		r := g.decide(t, id, step.action)
		if r.Status != step.status || (step.state != "" && r.Body["status"] != step.state) {
			t.Fatalf("%s answered %d %v, want %d %s", step.action, r.Status, r.Body, step.status, step.state)
		}

		if step.status == http.StatusConflict && r.Code() != "invalid_transition" {
			t.Fatalf("%s gave the code %v", step.action, r.Code())
		}
	}

	// a rejected tip is final, and the refresh does not bring it back or change it
	g.hours(t, 9*24, 8*24+1, 100, 0.1)

	after, _ := g.list(t).Body["items"].([]any)
	if len(after) != 1 || after[0].(map[string]any)["status"] != "rejected" {
		t.Fatalf("the list is %v", after)
	}

	// another business sees nothing of it
	if r := other.e.Call(t, http.MethodGet, "/v1/recommendations/"+id, "", other.Session); r.Status != http.StatusNotFound {
		t.Fatalf("a foreign read answered %d", r.Status)
	}

	if r := other.decide(t, id, "apply"); r.Status != http.StatusNotFound {
		t.Fatalf("a foreign decision answered %d", r.Status)
	}

	if r := g.decide(t, "not-a-uuid", "apply"); r.Status != http.StatusNotFound {
		t.Fatalf("a bad id answered %d", r.Status)
	}
}

func TestAnAppliedTipShowsItsDayAndIsVerifiedAfterFourteenDays(t *testing.T) {
	e := testutil.New(t)
	g := seed(t, e, "budi@example.com", "ESP32-AABBCC")
	g.hours(t, 8*24, 1, 100, 0.1)

	items, _ := g.list(t).Body["items"].([]any)
	id, _ := items[0].(map[string]any)["id"].(string)

	r := g.decide(t, id, "apply")

	v, _ := r.Body["verification"].(map[string]any)
	if v["collectedDays"] != 1.0 || v["neededDays"] != 14.0 || r.Body["appliedAt"] == nil {
		t.Fatalf("a new applied tip is %v", r.Body)
	}

	// with no production records the check cannot run, so the tip stays applied after the 14 days
	g.e.Exec(t, "UPDATE tips SET applied_at = now() - interval '15 days'")
	g.e.Exec(t, "UPDATE businesses SET tips_refreshed_at = NULL")

	if r := g.e.Call(t, http.MethodGet, "/v1/recommendations/"+id, "", g.Session); r.Body["status"] != "applied" {
		t.Fatalf("with no production the tip is %v", r.Body["status"])
	}

	// energy was higher before the tip, and every day made 100 pieces
	g.hours(t, 23*24, 8*24+1, 100, 0.2)
	g.e.Exec(t, "INSERT INTO production_records (business_id, day, quantity, unit) SELECT $1, (now() AT TIME ZONE 'Asia/Jakarta')::date - d, 100, 'potong' FROM generate_series(1, 24) d", g.Business)

	g.e.Exec(t, "UPDATE businesses SET tips_refreshed_at = NULL")

	d := g.e.Call(t, http.MethodGet, "/v1/recommendations/"+id, "", g.Session)
	result, _ := d.Body["result"].(map[string]any)

	if d.Body["status"] != "verified" || d.Body["verification"] != nil || result["beforeDays"] != 7.0 || result["afterDays"] != 14.0 {
		t.Fatalf("the verified tip is %v", d.Body)
	}

	before, _ := result["beforeKwh"].(float64)
	after, _ := result["afterKwh"].(float64)

	if before <= after || after <= 0 {
		t.Fatalf("the energy for each piece is %v before and %v after", before, after)
	}

	list, _ := g.list(t).Body["items"].([]any)

	item, _ := list[0].(map[string]any)
	if item["status"] != "verified" || item["resultPercent"] == nil || item["progress"] != nil {
		t.Fatalf("the verified row is %v", item)
	}

	// a verified tip is final
	if r := g.decide(t, id, "cancel"); r.Status != http.StatusConflict {
		t.Fatalf("cancel of a verified tip answered %d", r.Status)
	}
}

func TestAWaitingTipGoesAwayWhenTheDeviceStopsDrawingPowerAndTheListStaysAnArray(t *testing.T) {
	e := testutil.New(t)
	g := seed(t, e, "budi@example.com", "ESP32-AABBCC")
	g.hours(t, 8*24, 1, 100, 0.1)

	if items, _ := g.list(t).Body["items"].([]any); len(items) != 1 {
		t.Fatalf("the list has %d tips", len(items))
	}

	g.e.Exec(t, "UPDATE readings_hourly SET energy_kwh = 0, sum_watts = 0, max_watts = 0, min_watts = 0")

	r := g.list(t)

	items, ok := r.Body["items"].([]any)
	if r.Body["phase"] != "ready" || !ok || len(items) != 0 {
		t.Fatalf("a quiet device left %v", r.Body)
	}
}

func TestTheCheckCountsTheEnergyOnlyOfDaysWithProduction(t *testing.T) {
	e := testutil.New(t)
	g := seed(t, e, "budi@example.com", "ESP32-AABBCC")
	g.hours(t, 8*24, 1, 100, 0.1)

	items, _ := g.list(t).Body["items"].([]any)
	id, _ := items[0].(map[string]any)["id"].(string)
	g.decide(t, id, "apply")

	g.e.Exec(t, "UPDATE tips SET applied_at = now() - interval '15 days'")

	// the same energy every day before the tip, and production on only 3 of the 7 days before it, so the energy of the other days must
	// not be set against the pieces of the 3
	g.hours(t, 23*24, 8*24+1, 100, 0.2)
	g.e.Exec(t, "INSERT INTO production_records (business_id, day, quantity, unit) SELECT $1, (now() AT TIME ZONE 'Asia/Jakarta')::date - d, 100, 'potong' FROM generate_series(1, 14) d", g.Business)
	g.e.Exec(t, "INSERT INTO production_records (business_id, day, quantity, unit) SELECT $1, (now() AT TIME ZONE 'Asia/Jakarta')::date - d, 100, 'potong' FROM generate_series(17, 19) d", g.Business)
	g.e.Exec(t, "UPDATE businesses SET tips_refreshed_at = NULL")

	d := g.e.Call(t, http.MethodGet, "/v1/recommendations/"+id, "", g.Session)
	result, _ := d.Body["result"].(map[string]any)

	before, _ := result["beforeKwh"].(float64)
	after, _ := result["afterKwh"].(float64)

	// 3 days of 24 hours at 0,2 kWh for 300 pieces is 0,048 kWh for each piece, and the 14 days after are 0,1 kWh for each hour
	if before < 0.045 || before > 0.05 || after <= 0 || after >= before {
		t.Fatalf("the energy for each piece is %v before and %v after, %v", before, after, d.Body)
	}
}
