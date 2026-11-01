package device_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

// send sends one sample of a node with the given power, an age and an optional condition code of the node
func (g *rig) send(t *testing.T, node string, watts float64, age time.Duration, state *int) {
	t.Helper()

	g.seq++

	at := time.Now().Add(-age).Unix()
	extra := ""

	if state != nil {
		extra = fmt.Sprintf(`,"a":%d`, *state)
	}

	body := fmt.Sprintf(`{"seq":%d,"sentAt":%d,"nodes":[{"nodeId":"%s","kind":"plug","samples":[{"t":%d,"p":%g%s}]}]}`,
		g.seq, time.Now().Unix(), node, at, watts, extra)

	if r := g.e.CallWith(t, http.MethodPost, "/device/v1/readings", body, map[string]string{"Authorization": "Bearer " + g.key}); r.Status != http.StatusOK {
		t.Fatalf("readings answered %d %v", r.Status, r.Body)
	}
}

func (g *rig) anomalies(t *testing.T) int {
	t.Helper()

	var n int
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM anomalies").Scan(&n); err != nil {
		t.Fatal(err)
	}

	return n
}

// usual writes the same high power for the current hour of the day on the given number of earlier days
func (g *rig) usual(t *testing.T, days int, high float64) {
	t.Helper()

	g.e.Exec(t, "INSERT INTO readings_hourly (gateway_id, node_id, hour, samples, sum_watts, max_watts, min_watts) "+
		"SELECT n.gateway_id, n.node_id, date_trunc('hour', now()) - make_interval(days => d), 10, $2::float8 * 10, $2, 1 "+
		"FROM nodes n, generate_series(1, $1) d ON CONFLICT DO NOTHING", days, high)
}

func TestASurgeFarAboveTheUsualPowerRaisesOneAnomaly(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	id := g.pair(t, "PLUG-0001", "Mesin jahit 1")
	g.usual(t, 5, 100)

	// the usual high is 100 W, so the limit is 200 W, and 150 W is still normal
	g.send(t, "PLUG-0001", 150, 0, nil)

	if n := g.anomalies(t); n != 0 {
		t.Fatalf("150 W raised %d anomalies", n)
	}

	g.send(t, "PLUG-0001", 500, 0, nil)

	var kind string

	var peak, expected float64
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT kind, peak_watts, expected_high_watts FROM anomalies").Scan(&kind, &peak, &expected); err != nil {
		t.Fatalf("no anomaly was stored %v", err)
	}

	if kind != "surge" || peak != 500 || expected != 100 {
		t.Fatalf("the anomaly is %s %v %v", kind, peak, expected)
	}

	// a second surge in the next minutes is the same event
	g.send(t, "PLUG-0001", 900, 0, nil)

	if n := g.anomalies(t); n != 1 {
		t.Fatalf("a second surge made %d anomalies", n)
	}

	if d := g.device(t, id); d.Body["status"] != "anomali" {
		t.Fatalf("the device is %v", d.Body["status"])
	}
}

func TestTheNodeCanReportAnOverloadWithoutAnyHistory(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	g.pair(t, "PLUG-0001", "Mesin jahit 1")

	three := 3
	g.send(t, "PLUG-0001", 50, 0, &three)

	var kind string
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT kind FROM anomalies").Scan(&kind); err != nil || kind != "overload" {
		t.Fatalf("the anomaly is %q (%v)", kind, err)
	}
}

func TestNoSurgeWithoutEnoughHistoryAndNoneForOldOrUnclaimedReadings(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	g.pair(t, "PLUG-0001", "Mesin jahit 1")

	// two days are too few to know what is usual
	g.usual(t, 2, 100)
	g.send(t, "PLUG-0001", 900, 0, nil)

	if n := g.anomalies(t); n != 0 {
		t.Fatalf("two days of history raised %d anomalies", n)
	}

	g.usual(t, 5, 100)

	// a reading from 3 hours ago is history and not news
	g.send(t, "PLUG-0001", 900, 3*time.Hour, nil)

	if n := g.anomalies(t); n != 0 {
		t.Fatalf("an old reading raised %d anomalies", n)
	}

	// a node that no business claimed is not watched
	g.send(t, "PLUG-9999", 900, 0, nil)

	if n := g.anomalies(t); n != 0 {
		t.Fatalf("an unclaimed node raised %d anomalies", n)
	}
}
