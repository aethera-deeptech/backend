package device_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

// pair makes a plug a device of the business and returns the id of the device
func (g *rig) pair(t *testing.T, node, name string) string {
	t.Helper()

	r := g.e.Call(t, http.MethodPost, "/v1/pairings", `{"kind":"plug","name":"`+name+`","roomId":"`+g.room+`"}`, g.session)
	if r.Status != http.StatusCreated {
		t.Fatalf("start answered %d %v", r.Status, r.Body)
	}

	id, _ := r.Body["id"].(string)

	g.hear(t, node, "plug")

	s := g.status(t, id)

	device, _ := s.Body["deviceId"].(string)
	if device == "" {
		t.Fatalf("the plug was not paired %v", s.Body)
	}

	return device
}

func (g *rig) device(t *testing.T, id string) testutil.Reply {
	t.Helper()

	return g.e.Call(t, http.MethodGet, "/v1/devices/"+id, "", g.session)
}

func TestAFreshDeviceIsOnAndShowsItsPattern(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	id := g.pair(t, "PLUG-0001", "Mesin jahit 1")

	list := g.e.Call(t, http.MethodGet, "/v1/devices", "", g.session)

	items, _ := list.Body["items"].([]any)
	if list.Status != http.StatusOK || len(items) != 1 {
		t.Fatalf("the list answered %d %v", list.Status, list.Body)
	}

	row, _ := items[0].(map[string]any)
	if row["id"] != id || row["kind"] != "plug" || row["status"] != "nyala" || row["currentPowerW"] != 12.5 {
		t.Fatalf("the row is %v", row)
	}

	d := g.device(t, id)
	if d.Status != http.StatusOK {
		t.Fatalf("the detail answered %d %v", d.Status, d.Body)
	}

	if d.Body["room"] != "Ruang jahit" || d.Body["kind"] != "plug" || d.Body["kindLabel"] != "WATTLINK Plug" || d.Body["signal"] != "baik" || d.Body["status"] != "nyala" {
		t.Fatalf("the detail is %v", d.Body)
	}

	pattern, _ := d.Body["powerPattern"].(map[string]any)
	points, _ := pattern["points"].([]any)

	if len(points) != 96 || pattern["yMaxW"] != 20.0 {
		t.Fatalf("the pattern has %d points and the ceiling %v", len(points), pattern["yMaxW"])
	}

	// one reading fills one point, and every time with no reading is null and not zero
	filled := 0

	for _, p := range points {
		if p != nil {
			filled++
		}
	}

	if filled != 1 {
		t.Fatalf("%d points have a value, want 1 and null for the others", filled)
	}

	if _, ok := pattern["expected"]; ok {
		t.Fatal("a device with one reading has no usual band")
	}
}

func TestAQuietDeviceIsOfflineAndKeepsItsMonth(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	id := g.pair(t, "PLUG-0001", "Mesin jahit 1")

	g.e.Exec(t, "UPDATE nodes SET last_sample_at = now() - interval '1 hour'")
	g.e.Exec(t, "INSERT INTO readings_hourly (gateway_id, node_id, hour, samples, sum_watts, max_watts, min_watts, energy_kwh) "+
		"SELECT gateway_id, node_id, date_trunc('hour', now()), 1, 12.5, 12.5, 12.5, 0.5 FROM nodes ON CONFLICT (gateway_id, node_id, hour) DO UPDATE SET energy_kwh = 0.5")

	d := g.device(t, id)
	if d.Body["status"] != "offline" || d.Body["currentPowerW"] != nil || d.Body["todayKwh"] != nil || d.Body["signal"] != "tidak-ada" {
		t.Fatalf("the offline detail is %v", d.Body)
	}

	if d.Body["monthKwh"] != 0.5 || d.Body["lastSeenAt"] == nil {
		t.Fatalf("the month or the last time is wrong %v", d.Body)
	}

	items, _ := g.e.Call(t, http.MethodGet, "/v1/devices", "", g.session).Body["items"].([]any)
	row, _ := items[0].(map[string]any)

	if row["status"] != "offline" || row["lastSeenAt"] == nil || row["currentPowerW"] != nil {
		t.Fatalf("the offline row is %v", row)
	}
}

func TestAnAnomalyShowsForADayAndTheListPutsItFirst(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	first := g.pair(t, "PLUG-0001", "Alpha")
	second := g.pair(t, "PLUG-0002", "Zulu")

	g.e.Exec(t, "INSERT INTO anomalies (gateway_id, node_id, at, peak_watts, expected_high_watts, kind) VALUES ((SELECT gateway_id FROM devices WHERE id = $1), 'PLUG-0002', now() - interval '1 hour', 900, 300, 'surge')", second)

	d := g.device(t, second)
	anomaly, _ := d.Body["anomaly"].(map[string]any)

	if d.Body["status"] != "anomali" || anomaly["peakW"] != 900.0 {
		t.Fatalf("the anomaly detail is %v", d.Body)
	}

	items, _ := g.e.Call(t, http.MethodGet, "/v1/devices", "", g.session).Body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("the list has %d rows", len(items))
	}

	a, _ := items[0].(map[string]any)
	b, _ := items[1].(map[string]any)

	if a["id"] != second || a["anomalyAt"] == nil || b["id"] != first {
		t.Fatalf("the order is %v then %v", a, b)
	}

	// an anomaly that is older than a day does not count
	g.e.Exec(t, "UPDATE anomalies SET at = now() - interval '25 hours'")

	if d := g.device(t, second); d.Body["status"] == "anomali" || d.Body["anomaly"] != nil {
		t.Fatalf("an old anomaly still counts %v", d.Body)
	}
}

func TestTheUsualBandNeedsEveryHourOfTheDayOnSeveralDays(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	id := g.pair(t, "PLUG-0001", "Mesin jahit 1")

	// 5 earlier days of every hour, each hour with a high of 100 W
	g.e.Exec(t, "INSERT INTO readings_hourly (gateway_id, node_id, hour, samples, sum_watts, max_watts, min_watts) "+
		"SELECT n.gateway_id, n.node_id, date_trunc('hour', now()) - make_interval(hours => h), 10, 500, 100, 20 FROM nodes n, generate_series(25, 25 + 5 * 24 - 1) h")

	pattern, _ := g.device(t, id).Body["powerPattern"].(map[string]any)
	band, _ := pattern["expected"].(map[string]any)
	high, _ := band["high"].([]any)

	if len(high) != 96 || high[0] != 100.0 {
		t.Fatalf("the band is %v", band)
	}

	// the standard deviation is zero, so low and high meet
	low, _ := band["low"].([]any)
	if low[0] != 100.0 {
		t.Fatalf("the low side is %v", low[0])
	}

	// a day with a hole in it leaves the band out
	g.e.Exec(t, "DELETE FROM readings_hourly WHERE extract(hour FROM hour AT TIME ZONE 'UTC') = 3 AND hour < now() - interval '24 hours'")

	pattern, _ = g.device(t, id).Body["powerPattern"].(map[string]any)
	if _, ok := pattern["expected"]; ok {
		t.Fatal("a band was built with no data for one hour of the day")
	}
}

func TestRenameChecksTheNameAndTheOwner(t *testing.T) {
	e := testutil.New(t)
	g := business(t, e, "budi@example.com", "ESP32-AABBCC", "a")
	other := business(t, e, "sari@example.com", "ESP32-DDEEFF", "b")
	id := g.pair(t, "PLUG-0001", "Mesin jahit 1")

	if r := e.Call(t, http.MethodPatch, "/v1/devices/"+id, `{"name":"  Mesin obras  "}`, g.session); r.Status != http.StatusOK || r.Body["name"] != "Mesin obras" {
		t.Fatalf("the rename answered %d %v", r.Status, r.Body)
	}

	for _, body := range []string{`{"name":""}`, `{"name":"   "}`, `{"name":"` + strings.Repeat("x", 41) + `"}`} {
		if r := e.Call(t, http.MethodPatch, "/v1/devices/"+id, body, g.session); r.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s answered %d", body, r.Status)
		}
	}

	// another business sees nothing and changes nothing, and both answers are the same as for an unknown id
	if r := e.Call(t, http.MethodPatch, "/v1/devices/"+id, `{"name":"Mine"}`, other.session); r.Status != http.StatusNotFound {
		t.Fatalf("a foreign rename answered %d", r.Status)
	}

	if r := other.device(t, id); r.Status != http.StatusNotFound {
		t.Fatalf("a foreign read answered %d", r.Status)
	}

	if r := other.device(t, "not-a-uuid"); r.Status != http.StatusNotFound {
		t.Fatalf("a bad id answered %d", r.Status)
	}

	items, _ := e.Call(t, http.MethodGet, "/v1/devices", "", other.session).Body["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("the other business sees %d devices", len(items))
	}

	if r := e.Call(t, http.MethodGet, "/v1/devices", "", ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("no session answered %d", r.Status)
	}
}

func TestAClampShowsItsKindInTheListAndTheDetail(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")

	r := g.start(t, "clamp")
	if r.Status != http.StatusCreated {
		t.Fatalf("start answered %d %v", r.Status, r.Body)
	}

	pairing, _ := r.Body["id"].(string)

	g.hear(t, "CLMP-0001", "clamp")

	id, _ := g.status(t, pairing).Body["deviceId"].(string)
	if id == "" {
		t.Fatal("the clamp was not paired")
	}

	items, _ := g.e.Call(t, http.MethodGet, "/v1/devices", "", g.session).Body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("the list has %d items", len(items))
	}

	if row, _ := items[0].(map[string]any); row["kind"] != "clamp" {
		t.Fatalf("the row is %v", row)
	}

	if d := g.device(t, id); d.Body["kind"] != "clamp" || d.Body["kindLabel"] != "WATTLINK Clamp (dijepit di kabel)" {
		t.Fatalf("the detail is %v", d.Body)
	}
}

func TestAnEnergyResetStartsTheTotalAgainAndKeepsTheMonth(t *testing.T) {
	e := testutil.New(t)
	g := business(t, e, "budi@example.com", "ESP32-AABBCC", "a")
	other := business(t, e, "sari@example.com", "ESP32-DDEEFF", "b")
	id := g.pair(t, "PLUG-0001", "Mesin jahit 1")

	// three hours that were before the reset, the hour that the reset falls in, and an hour that begins after it
	for _, row := range []struct {
		hours string
		kwh   float64
	}{{"-5 hours", 1.0}, {"-3 hours", 2.0}, {"-1 hour", 0.5}, {"0 hours", 0.3}, {"+2 hours", 0.7}} {
		e.Exec(t, "INSERT INTO readings_hourly (gateway_id, node_id, hour, samples, sum_watts, max_watts, min_watts, energy_kwh) "+
			"SELECT gateway_id, node_id, date_trunc('hour', now()) + $1::interval, 1, 12.5, 12.5, 12.5, $2 FROM nodes "+
			"ON CONFLICT (gateway_id, node_id, hour) DO UPDATE SET energy_kwh = EXCLUDED.energy_kwh", row.hours, row.kwh)
	}

	var rowsBefore int
	if err := e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM readings_hourly").Scan(&rowsBefore); err != nil {
		t.Fatalf("count the hourly rows %v", err)
	}

	before := g.device(t, id)

	since, _ := before.Body["energySince"].(map[string]any)
	if since["resetAt"] != nil || since["kwh"] != 4.5 {
		t.Fatalf("with no reset the total is %v", before.Body["energySince"])
	}

	r := e.Call(t, http.MethodPost, "/v1/devices/"+id+"/energy-reset", "", g.session)
	if r.Status != http.StatusOK || r.Body["resetAt"] == nil {
		t.Fatalf("the reset answered %d %v", r.Status, r.Body)
	}

	after := g.device(t, id)

	since, _ = after.Body["energySince"].(map[string]any)
	if since["resetAt"] != r.Body["resetAt"] || since["kwh"] != 0.7 {
		t.Fatalf("after the reset the total is %v, the reset was %v", after.Body["energySince"], r.Body["resetAt"])
	}

	// the day and the month feed the bill, the tips and the reports, so a reset must not move them
	if after.Body["monthKwh"] != before.Body["monthKwh"] || after.Body["todayKwh"] != before.Body["todayKwh"] {
		t.Fatalf("the reset moved the month or the day, before %v after %v", before.Body, after.Body)
	}

	var rowsAfter int
	if err := e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM readings_hourly").Scan(&rowsAfter); err != nil {
		t.Fatalf("count the hourly rows %v", err)
	}

	if rowsAfter != rowsBefore {
		t.Fatalf("the reset changed the hourly rows from %d to %d", rowsBefore, rowsAfter)
	}

	// a second reset replaces the time
	if second := e.Call(t, http.MethodPost, "/v1/devices/"+id+"/energy-reset", "", g.session); second.Status != http.StatusOK || second.Body["resetAt"] == r.Body["resetAt"] {
		t.Fatalf("the second reset answered %d %v after %v", second.Status, second.Body, r.Body["resetAt"])
	}

	// another business and a wrong id get the same answer, and nothing changes
	for _, path := range []string{"/v1/devices/" + id + "/energy-reset", "/v1/devices/not-a-uuid/energy-reset", "/v1/devices/6f1c1c1e-0000-4000-8000-000000000000/energy-reset"} {
		if r := e.Call(t, http.MethodPost, path, "", other.session); r.Status != http.StatusNotFound {
			t.Fatalf("%s answered %d", path, r.Status)
		}
	}

	if r := e.Call(t, http.MethodPost, "/v1/devices/"+id+"/energy-reset", "", ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("a reset with no session answered %d", r.Status)
	}
}
