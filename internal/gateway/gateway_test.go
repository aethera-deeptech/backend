package gateway_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

// key makes a gateway key of the right length and the hash that the gateway sends when it enrolls
func key(seed string) (string, string) {
	k := strings.Repeat(seed, 43)[:43]
	sum := sha256.Sum256([]byte(k))

	return k, hex.EncodeToString(sum[:])
}

func bearer(k string) map[string]string { return map[string]string{"Authorization": "Bearer " + k} }

type rig struct {
	e       *testutil.Env
	session string
	key     string
	gateway string
}

// enrolled signs a business up, enrolls a gateway with a key and claims it
func enrolled(t *testing.T, email, hardware, seed string) rig {
	t.Helper()

	e := testutil.New(t)

	return enrolledOn(t, e, email, hardware, seed)
}

func enrolledOn(t *testing.T, e *testutil.Env, email, hardware, seed string) rig {
	t.Helper()

	session := e.SignUp(t, email)
	k, hash := key(seed)

	r := e.CallWith(t, http.MethodPost, "/device/v1/enroll", `{"hardwareId":"`+hardware+`","firmware":"0.1.0","keyHash":"`+hash+`"}`, nil)
	if r.Status != http.StatusCreated {
		t.Fatalf("enroll answered %d %v", r.Status, r.Body)
	}

	code, _ := r.Body["claimCode"].(string)
	poll, _ := r.Body["pollId"].(string)

	if pending := e.CallWith(t, http.MethodGet, "/device/v1/enroll/"+poll, "", nil); pending.Status != http.StatusAccepted {
		t.Fatalf("poll before the claim answered %d", pending.Status)
	}

	r = e.Call(t, http.MethodPost, "/v1/gateways", `{"claimCode":"`+code+`","name":"Hub ruang jahit"}`, session)
	if r.Status != http.StatusCreated {
		t.Fatalf("claim answered %d %v", r.Status, r.Body)
	}

	id, _ := r.Body["id"].(string)

	if done := e.CallWith(t, http.MethodGet, "/device/v1/enroll/"+poll, "", nil); done.Status != http.StatusOK || done.Body["gatewayId"] != id {
		t.Fatalf("poll after the claim answered %d %v", done.Status, done.Body)
	}

	// the answer comes once
	if again := e.CallWith(t, http.MethodGet, "/device/v1/enroll/"+poll, "", nil); again.Status != http.StatusNotFound {
		t.Fatalf("a second poll answered %d, want 404", again.Status)
	}

	return rig{e: e, session: session, key: k, gateway: id}
}

func (g rig) send(t *testing.T, body string) testutil.Reply {
	t.Helper()

	return g.e.CallWith(t, http.MethodPost, "/device/v1/readings", body, bearer(g.key))
}

func batch(seq int, node, kind string, samples ...string) string {
	return fmt.Sprintf(`{"seq":%d,"sentAt":%d,"nodes":[{"nodeId":"%s","kind":"%s","samples":[%s]}]}`, seq, time.Now().Unix(), node, kind, strings.Join(samples, ","))
}

func sample(ago int, watts float64) string {
	return fmt.Sprintf(`{"t":%d,"p":%v,"v":220,"i":1.5,"e":12.5,"f":50,"pf":0.9,"a":0}`, time.Now().Add(-time.Duration(ago)*time.Second).Unix(), watts)
}

func TestEnrollClaimAndSendReadings(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "a")

	r := g.send(t, batch(1, "NODE-0001", "plug", sample(120, 310), sample(60, 305), sample(0, 2000)))
	if r.Status != http.StatusOK || r.Body["ackSeq"] != float64(1) || r.Body["accepted"] != float64(3) || r.Body["rejected"] != float64(0) {
		t.Fatalf("answered %d %v", r.Status, r.Body)
	}

	var n int
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM readings").Scan(&n); err != nil || n != 3 {
		t.Fatalf("%d readings are stored (%v), want 3", n, err)
	}

	list := g.e.Call(t, http.MethodGet, "/v1/gateways", "", g.session)
	items, _ := list.Body["items"].([]any)

	if len(items) != 1 || items[0].(map[string]any)["lastSeenAt"] == nil {
		t.Fatalf("the list is %v", list.Body)
	}
}

func TestACodeWorksOnceAndOnlyForTheRightFormat(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")
	_, hash := key("b")

	r := e.CallWith(t, http.MethodPost, "/device/v1/enroll", `{"hardwareId":"ESP32-112233","keyHash":"`+hash+`"}`, nil)
	code, _ := r.Body["claimCode"].(string)

	if len(code) != 8 || strings.ContainsAny(code, "01IO") {
		t.Fatalf("the claim code %q has a look-alike character or a wrong length", code)
	}

	// lower case and spaces are fine, because the code is read from a display and typed
	if r := e.Call(t, http.MethodPost, "/v1/gateways", `{"claimCode":" `+strings.ToLower(code)+` ","name":"Hub"}`, session); r.Status != http.StatusCreated {
		t.Fatalf("claim answered %d %v", r.Status, r.Body)
	}

	if r := e.Call(t, http.MethodPost, "/v1/gateways", `{"claimCode":"`+code+`","name":"Hub dua"}`, session); r.Status != http.StatusNotFound || r.Code() != "claim_code_unknown" {
		t.Fatalf("a used code answered %d %v", r.Status, r.Body)
	}

	for _, bad := range []string{"SHORT", "ABCDEFGH1", "abc!defg"} {
		if r := e.Call(t, http.MethodPost, "/v1/gateways", `{"claimCode":"`+bad+`","name":"Hub"}`, session); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("the code %q answered %d, want 422", bad, r.Status)
		}
	}
}

func TestAnExpiredCodeIsRefused(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")
	_, hash := key("c")

	r := e.CallWith(t, http.MethodPost, "/device/v1/enroll", `{"hardwareId":"ESP32-445566","keyHash":"`+hash+`"}`, nil)
	code, _ := r.Body["claimCode"].(string)
	poll, _ := r.Body["pollId"].(string)

	e.Exec(t, "UPDATE gateway_enrollments SET expires_at = now() - interval '1 second'")

	if r := e.Call(t, http.MethodPost, "/v1/gateways", `{"claimCode":"`+code+`","name":"Hub"}`, session); r.Status != http.StatusNotFound {
		t.Fatalf("an expired code answered %d", r.Status)
	}

	if r := e.CallWith(t, http.MethodGet, "/device/v1/enroll/"+poll, "", nil); r.Status != http.StatusGone || r.Code() != "enrollment_expired" {
		t.Fatalf("a poll of an expired code answered %d %v", r.Status, r.Body)
	}
}

func TestEnrollValidation(t *testing.T) {
	e := testutil.New(t)
	_, hash := key("d")

	tests := map[string]string{
		"short hardware id": `{"hardwareId":"ab","keyHash":"` + hash + `"}`,
		"odd characters":    `{"hardwareId":"ESP32 AABBCC!","keyHash":"` + hash + `"}`,
		"hash in capitals":  `{"hardwareId":"ESP32-AABBCC","keyHash":"` + strings.ToUpper(hash) + `"}`,
		"short hash":        `{"hardwareId":"ESP32-AABBCC","keyHash":"abcd"}`,
		"line break":        `{"hardwareId":"ESP32-AABBCC","firmware":"1\r\n2","keyHash":"` + hash + `"}`,
		"no hash":           `{"hardwareId":"ESP32-AABBCC"}`,
	}

	for name, body := range tests {
		if r := e.CallWith(t, http.MethodPost, "/device/v1/enroll", body, nil); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s answered %d, want 422", name, r.Status)
		}
	}

	if r := e.CallWith(t, http.MethodPost, "/device/v1/enroll", `{"hardwareId":"ESP32-AABBCC","keyHash":"`+hash+`","x":1}`, nil); r.Status != http.StatusBadRequest {
		t.Errorf("an unknown field answered %d, want 400", r.Status)
	}

	if r := e.CallWith(t, http.MethodGet, "/device/v1/enroll/short", "", nil); r.Status != http.StatusNotFound {
		t.Errorf("a short poll id answered %d, want 404", r.Status)
	}
}

func TestOnlyTheKeyOfAClaimedGatewayWorks(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "e")

	tests := map[string]map[string]string{
		"no header":      nil,
		"a wrong key":    bearer(strings.Repeat("z", 43)),
		"another scheme": {"Authorization": "Basic " + g.key},
		"a short key":    bearer("abc"),
		"a long key":     bearer(strings.Repeat("a", 200)),
	}

	for name, headers := range tests {
		r := g.e.CallWith(t, http.MethodPost, "/device/v1/readings", batch(1, "NODE-0001", "plug", sample(0, 5)), headers)
		if r.Status != http.StatusUnauthorized || r.Code() != "invalid_gateway_key" {
			t.Errorf("%s answered %d %v", name, r.Status, r.Body)
		}
	}

	// the hash of the key is not the key
	_, hash := key("e")
	if r := g.e.CallWith(t, http.MethodPost, "/device/v1/readings", batch(1, "NODE-0001", "plug", sample(0, 5)), bearer(hash)); r.Status != http.StatusUnauthorized {
		t.Errorf("the hash of the key worked as a key and answered %d", r.Status)
	}

	var n int
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM readings").Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d readings were stored by a request without a key", n)
	}
}

func TestSequenceNumbers(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "f")

	if r := g.send(t, batch(1, "NODE-0001", "plug", sample(10, 5))); r.Status != http.StatusOK {
		t.Fatalf("batch 1 answered %d", r.Status)
	}

	// a retry of the same batch is answered again and stores nothing
	if r := g.send(t, batch(1, "NODE-0001", "plug", sample(10, 5), sample(5, 6))); r.Status != http.StatusOK || r.Body["ackSeq"] != float64(1) || r.Body["accepted"] != float64(0) {
		t.Fatalf("the retry answered %d %v", r.Status, r.Body)
	}

	// a batch that skips a number says which one the service waits for
	r := g.send(t, batch(5, "NODE-0001", "plug", sample(0, 7)))
	if r.Status != http.StatusConflict || r.Code() != "sequence_gap" || r.Body["expectedSeq"] != float64(2) {
		t.Fatalf("the gap answered %d %v", r.Status, r.Body)
	}

	if r := g.send(t, batch(2, "NODE-0001", "plug", sample(0, 7))); r.Status != http.StatusOK || r.Body["ackSeq"] != float64(2) {
		t.Fatalf("batch 2 answered %d %v", r.Status, r.Body)
	}

	var n int
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM readings").Scan(&n); err != nil || n != 2 {
		t.Fatalf("%d readings are stored (%v), want 2", n, err)
	}
}

func TestBadSamplesAreDroppedAndTheRestIsStored(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "g")

	old := fmt.Sprintf(`{"t":%d,"p":10}`, time.Now().Add(-25*time.Hour).Unix())
	future := fmt.Sprintf(`{"t":%d,"p":10}`, time.Now().Add(10*time.Minute).Unix())
	noPower := fmt.Sprintf(`{"t":%d,"v":220}`, time.Now().Unix())
	tooHigh := fmt.Sprintf(`{"t":%d,"p":70000}`, time.Now().Unix())
	negative := fmt.Sprintf(`{"t":%d,"p":-5}`, time.Now().Unix())
	badState := fmt.Sprintf(`{"t":%d,"p":5,"a":9}`, time.Now().Unix())

	r := g.send(t, batch(1, "NODE-0001", "plug", sample(1, 40), old, future, noPower, tooHigh, negative, badState))
	if r.Status != http.StatusOK || r.Body["accepted"] != float64(1) || r.Body["rejected"] != float64(6) {
		t.Fatalf("answered %d %v", r.Status, r.Body)
	}
}

func TestTheTimeStampsFollowTheClockOfTheService(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "h")

	// the gateway has not asked a time server, so its clock is one day and a few hours behind. The samples are one minute apart.
	behind := time.Now().Add(-26 * time.Hour)
	body := fmt.Sprintf(`{"seq":1,"sentAt":%d,"nodes":[{"nodeId":"NODE-0001","kind":"plug","samples":[{"t":%d,"p":10},{"t":%d,"p":20}]}]}`,
		behind.Unix(), behind.Add(-60*time.Second).Unix(), behind.Unix())

	if r := g.send(t, body); r.Status != http.StatusOK || r.Body["accepted"] != float64(2) {
		t.Fatalf("answered %d %v", r.Status, r.Body)
	}

	var newest, oldest time.Time
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT max(t), min(t) FROM readings").Scan(&newest, &oldest); err != nil {
		t.Fatal(err)
	}

	if d := time.Since(newest); d < 0 || d > time.Minute {
		t.Fatalf("the newest sample is %v old, want about now", d)
	}

	if gap := newest.Sub(oldest); gap < 55*time.Second || gap > 65*time.Second {
		t.Fatalf("the samples are %v apart, want one minute", gap)
	}
}

func TestBatchShape(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "i")
	now := time.Now().Unix()

	tests := map[string]string{
		"seq zero":     fmt.Sprintf(`{"seq":0,"sentAt":%d,"nodes":[{"nodeId":"NODE-0001","kind":"plug","samples":[{"t":%d,"p":1}]}]}`, now, now),
		"no nodes":     fmt.Sprintf(`{"seq":1,"sentAt":%d,"nodes":[]}`, now),
		"a bad sentAt": fmt.Sprintf(`{"seq":1,"sentAt":5,"nodes":[{"nodeId":"NODE-0001","kind":"plug","samples":[{"t":%d,"p":1}]}]}`, now),
	}

	for name, body := range tests {
		if r := g.send(t, body); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s answered %d %v, want 422", name, r.Status, r.Body)
		}
	}

	// none of the refused batches moved the sequence number
	if r := g.send(t, batch(1, "NODE-0001", "plug", sample(0, 5))); r.Status != http.StatusOK {
		t.Fatalf("batch 1 answered %d after the refusals", r.Status)
	}

	var parts []string
	for i := range 1001 {
		parts = append(parts, fmt.Sprintf(`{"t":%d,"p":1}`, now-int64(i)))
	}

	// the samples of one node over the limit are dropped with the node, and the batch moves on
	if r := g.send(t, batch(2, "NODE-0001", "plug", parts...)); r.Status != http.StatusOK || r.Body["rejected"] != 1001.0 || r.Body["accepted"] != 0.0 {
		t.Errorf("1001 samples answered %d %v, want 200 with 1001 rejected", r.Status, r.Body)
	}

	if r := g.send(t, `{"seq":3,"sentAt":`+fmt.Sprint(now)+`,"nodes":[{"nodeId":"NODE-0001","kind":"plug","samples":[{"t":1,"p":1}]}], "extra":1}`); r.Status != http.StatusBadRequest {
		t.Errorf("an unknown field answered %d, want 400", r.Status)
	}
}

func TestABigBodyIsRefused(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "j")

	big := `{"seq":1,"sentAt":1800000000,"pad":"` + strings.Repeat("a", 300<<10) + `"}`
	if r := g.send(t, big); r.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("a body over 256 KiB answered %d, want 413", r.Status)
	}
}

func TestAnotherBusinessCannotTakeTheGatewayOfThisOne(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "k")
	other := g.e.SignUp(t, "ani@example.com")
	_, hash := key("l")

	r := g.e.CallWith(t, http.MethodPost, "/device/v1/enroll", `{"hardwareId":"ESP32-AABBCC","keyHash":"`+hash+`"}`, nil)
	code, _ := r.Body["claimCode"].(string)

	if r := g.e.Call(t, http.MethodPost, "/v1/gateways", `{"claimCode":"`+code+`","name":"Curian"}`, other); r.Status != http.StatusConflict || r.Code() != "gateway_claimed_elsewhere" {
		t.Fatalf("another business answered %d %v", r.Status, r.Body)
	}

	// the owner can claim it again with a new key and keeps the readings
	g.send(t, batch(1, "NODE-0001", "plug", sample(0, 5)))

	r = g.e.CallWith(t, http.MethodPost, "/device/v1/enroll", `{"hardwareId":"ESP32-AABBCC","keyHash":"`+hash+`"}`, nil)
	code, _ = r.Body["claimCode"].(string)

	if r := g.e.Call(t, http.MethodPost, "/v1/gateways", `{"claimCode":"`+code+`","name":"Hub baru"}`, g.session); r.Status != http.StatusCreated || r.Body["id"] != g.gateway {
		t.Fatalf("the owner answered %d %v", r.Status, r.Body)
	}

	if r := g.send(t, batch(2, "NODE-0001", "plug", sample(0, 5))); r.Status != http.StatusUnauthorized {
		t.Fatalf("the old key still works and answered %d", r.Status)
	}

	newKey, _ := key("l")

	if r := g.e.CallWith(t, http.MethodPost, "/device/v1/readings", batch(2, "NODE-0001", "plug", sample(0, 5)), bearer(newKey)); r.Status != http.StatusOK {
		t.Fatalf("the new key answered %d %v", r.Status, r.Body)
	}

	// a gateway that lost its counter, for example after a reset, is told where to continue and nothing is stored behind a silent answer
	r2 := g.e.CallWith(t, http.MethodPost, "/device/v1/readings", batch(1, "NODE-0001", "plug", sample(0, 5)), bearer(newKey))
	if r2.Status != http.StatusConflict || r2.Body["expectedSeq"] != float64(3) {
		t.Fatalf("an old number answered %d %v, want 409 with expectedSeq 3", r2.Status, r2.Body)
	}
}

func TestTheRoutesOfTheAppNeedASession(t *testing.T) {
	e := testutil.New(t)

	for _, route := range [][2]string{{http.MethodGet, "/v1/gateways"}, {http.MethodPost, "/v1/gateways"}} {
		if r := e.Call(t, route[0], route[1], `{}`, ""); r.Status != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d without a session", route[0], route[1], r.Status)
		}
	}
}

func TestABadNodeDoesNotBlockTheBatch(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "m")
	now := time.Now().Unix()

	body := fmt.Sprintf(`{"seq":1,"sentAt":%d,"nodes":[`+
		`{"nodeId":"NODE-0001","kind":"plug","samples":[{"t":%d,"p":5}]},`+
		`{"nodeId":"x","kind":"plug","samples":[{"t":%d,"p":1},{"t":%d,"p":1}]},`+
		`{"nodeId":"NODE-0002","kind":"toaster","samples":[{"t":%d,"p":1}]},`+
		`{"nodeId":"NODE-0003","kind":"plug","hops":9,"samples":[{"t":%d,"p":1}]},`+
		`{"nodeId":"NODE-0004","kind":"plug","samples":[]},`+
		`{"nodeId":"NODE-0001","kind":"plug","samples":[{"t":%d,"p":1}]}]}`, now, now, now, now, now, now, now)

	r := g.send(t, body)
	if r.Status != http.StatusOK || r.Body["accepted"] != 1.0 || r.Body["rejected"] != 5.0 {
		t.Fatalf("the batch answered %d %v, want 1 accepted and 5 rejected", r.Status, r.Body)
	}

	var n int
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM nodes").Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d nodes were stored (%v), want 1", n, err)
	}
}

func TestAGatewayCannotMakeUpMoreNodesThanTheLimit(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "n")
	now := time.Now().Unix()
	seq := 0

	for batchNo := range 6 {
		var nodes []string

		for i := range 16 {
			nodes = append(nodes, fmt.Sprintf(`{"nodeId":"NODE-%02d%02d","kind":"plug","samples":[{"t":%d,"p":1}]}`, batchNo, i, now))
		}

		seq++

		body := fmt.Sprintf(`{"seq":%d,"sentAt":%d,"nodes":[%s]}`, seq, now, strings.Join(nodes, ","))
		if r := g.send(t, body); r.Status != http.StatusOK {
			t.Fatalf("batch %d answered %d %v", seq, r.Status, r.Body)
		}
	}

	var n int
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM nodes").Scan(&n); err != nil || n != 64 {
		t.Fatalf("the gateway has %d nodes (%v), want the limit of 64", n, err)
	}
}

func TestACompressedBodyIsRefused(t *testing.T) {
	g := enrolled(t, "budi@example.com", "ESP32-AABBCC", "o")

	r := g.e.CallWith(t, http.MethodPost, "/device/v1/enroll", `{}`, map[string]string{"Content-Encoding": "gzip"})
	if r.Status != http.StatusUnsupportedMediaType {
		t.Fatalf("a compressed enroll answered %d", r.Status)
	}

	r = g.e.CallWith(t, http.MethodPost, "/device/v1/readings", `{}`, map[string]string{"Content-Encoding": "gzip", "Authorization": "Bearer " + g.key})
	if r.Status != http.StatusUnsupportedMediaType {
		t.Fatalf("compressed readings answered %d", r.Status)
	}
}
