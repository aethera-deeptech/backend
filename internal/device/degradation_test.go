package device_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

// load is one sample with its power and its power factor
type load struct{ watts, pf float64 }

// sendLoad sends the samples of one node in one batch, each one second after the one before, ending a little while ago so no reading
// meets the pairing reading or the samples of another call
func (g *rig) sendLoad(t *testing.T, node string, samples []load) {
	t.Helper()

	g.seq++

	end := time.Now().Add(-time.Duration(g.seq) * 30 * time.Second)
	parts := make([]string, 0, len(samples))

	for i, s := range samples {
		at := end.Add(-time.Duration(len(samples)-i) * time.Second).Unix()
		parts = append(parts, fmt.Sprintf(`{"t":%d,"p":%g,"pf":%g}`, at, s.watts, s.pf))
	}

	body := fmt.Sprintf(`{"seq":%d,"sentAt":%d,"nodes":[{"nodeId":"%s","kind":"plug","samples":[%s]}]}`, g.seq, time.Now().Unix(), node, strings.Join(parts, ","))

	if r := g.e.CallWith(t, http.MethodPost, "/device/v1/readings", body, map[string]string{"Authorization": "Bearer " + g.key}); r.Status != http.StatusOK {
		t.Fatalf("readings answered %d %v", r.Status, r.Body)
	}
}

func repeat(n int, s load) []load {
	out := make([]load, n)
	for i := range out {
		out[i] = s
	}

	return out
}

func TestAMotorUnderLoadWithAPoorPowerFactorRaisesADegradation(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	id := g.pair(t, "PLUG-0001", "Kompresor")

	g.sendLoad(t, "PLUG-0001", repeat(5, load{watts: 400, pf: 0.45}))

	var (
		kind string
		peak float64
		pf   *float64
	)

	if err := g.e.Pool.QueryRow(t.Context(), "SELECT kind, peak_watts, power_factor FROM anomalies").Scan(&kind, &peak, &pf); err != nil {
		t.Fatalf("no anomaly was stored %v", err)
	}

	if kind != "degradation" || peak != 400 || pf == nil || *pf < 0.44 || *pf > 0.46 {
		t.Fatalf("the anomaly is %s %v %v", kind, peak, pf)
	}

	// the same event in the next batch raises nothing new
	g.sendLoad(t, "PLUG-0001", repeat(5, load{watts: 400, pf: 0.45}))

	if n := g.anomalies(t); n != 1 {
		t.Fatalf("a second batch made %d anomalies", n)
	}

	d := g.device(t, id)
	anomaly, _ := d.Body["anomaly"].(map[string]any)

	if d.Body["status"] != "anomali" || anomaly["kind"] != "degradation" || anomaly["powerFactor"] != 0.45 || anomaly["peakW"] != 400.0 {
		t.Fatalf("the device is %v %v", d.Body["status"], anomaly)
	}
}

func TestNoDegradationForAGoodMotorALightLoadOrTooFewSamples(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	g.pair(t, "PLUG-0001", "Kompresor")

	cases := map[string][]load{
		"a good power factor":            repeat(5, load{watts: 400, pf: 0.9}),
		"a light load":                   repeat(5, load{watts: 20, pf: 0.3}),
		"too few loaded samples":         repeat(2, load{watts: 400, pf: 0.3}),
		"only half of the samples poor":  append(repeat(3, load{watts: 400, pf: 0.4}), repeat(3, load{watts: 400, pf: 0.9})...),
		"a poor sample at the load edge": {{watts: 34, pf: 0.2}, {watts: 34, pf: 0.2}, {watts: 34, pf: 0.2}},
	}

	for name, samples := range cases {
		g.sendLoad(t, "PLUG-0001", samples)

		if n := g.anomalies(t); n != 0 {
			t.Fatalf("%s raised %d anomalies", name, n)
		}
	}
}

func TestTheNodeCanReportADegradationItself(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	id := g.pair(t, "PLUG-0001", "Kompresor")

	two := 2
	g.send(t, "PLUG-0001", 50, 0, &two)

	var (
		kind string
		pf   *float64
	)

	if err := g.e.Pool.QueryRow(t.Context(), "SELECT kind, power_factor FROM anomalies").Scan(&kind, &pf); err != nil || kind != "degradation" || pf != nil {
		t.Fatalf("the anomaly is %q %v (%v)", kind, pf, err)
	}

	anomaly, _ := g.device(t, id).Body["anomaly"].(map[string]any)
	if anomaly["kind"] != "degradation" || anomaly["powerFactor"] != nil {
		t.Fatalf("the detail is %v", anomaly)
	}
}

func TestAReportedDegradationDoesNotKeepThePowerFactorOfOneSample(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")
	g.pair(t, "PLUG-0001", "Kompresor")

	g.seq++

	at := time.Now().Add(-time.Duration(g.seq) * 30 * time.Second).Unix()
	body := fmt.Sprintf(`{"seq":%d,"sentAt":%d,"nodes":[{"nodeId":"PLUG-0001","kind":"plug","samples":[{"t":%d,"p":80,"pf":0.8,"a":2}]}]}`, g.seq, time.Now().Unix(), at)

	if r := g.e.CallWith(t, http.MethodPost, "/device/v1/readings", body, map[string]string{"Authorization": "Bearer " + g.key}); r.Status != http.StatusOK {
		t.Fatalf("readings answered %d %v", r.Status, r.Body)
	}

	var (
		kind string
		pf   *float64
	)

	if err := g.e.Pool.QueryRow(t.Context(), "SELECT kind, power_factor FROM anomalies").Scan(&kind, &pf); err != nil || kind != "degradation" || pf != nil {
		t.Fatalf("the anomaly is %q %v (%v)", kind, pf, err)
	}
}
