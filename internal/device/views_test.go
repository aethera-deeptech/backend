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
	if row["id"] != id || row["status"] != "nyala" || row["currentPowerW"] != 12.5 {
		t.Fatalf("the row is %v", row)
	}

	d := g.device(t, id)
	if d.Status != http.StatusOK {
		t.Fatalf("the detail answered %d %v", d.Status, d.Body)
	}

	if d.Body["room"] != "Ruang jahit" || d.Body["kindLabel"] != "Colokan pintar" || d.Body["signal"] != "baik" || d.Body["status"] != "nyala" {
		t.Fatalf("the detail is %v", d.Body)
	}

	pattern, _ := d.Body["powerPattern"].(map[string]any)
	points, _ := pattern["points"].([]any)

	if len(points) != 96 || pattern["yMaxW"] != 20.0 {
		t.Fatalf("the pattern has %d points and the ceiling %v", len(points), pattern["yMaxW"])
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
