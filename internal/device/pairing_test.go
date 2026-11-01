package device_test

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

type rig struct {
	e       *testutil.Env
	session string
	key     string
	room    string
	seq     int
}

// business signs a business up, gives it a room and claims a gateway for it
func business(t *testing.T, e *testutil.Env, email, hardware, seed string) *rig {
	t.Helper()

	session := e.SignUp(t, email)
	k := strings.Repeat(seed, 43)[:43]
	sum := sha256.Sum256([]byte(k))

	r := e.CallWith(t, http.MethodPost, "/device/v1/enroll", `{"hardwareId":"`+hardware+`","keyHash":"`+hex.EncodeToString(sum[:])+`"}`, nil)
	code, _ := r.Body["claimCode"].(string)

	if r := e.Call(t, http.MethodPost, "/v1/gateways", `{"claimCode":"`+code+`","name":"Hub"}`, session); r.Status != http.StatusCreated {
		t.Fatalf("claim answered %d %v", r.Status, r.Body)
	}

	room := e.Call(t, http.MethodPost, "/v1/rooms", `{"name":"Ruang jahit"}`, session)
	id, _ := room.Body["id"].(string)

	return &rig{e: e, session: session, key: k, room: id}
}

// hear sends one reading of a node, so the gateway has heard it
func (g *rig) hear(t *testing.T, node, kind string) {
	t.Helper()

	g.seq++

	body := fmt.Sprintf(`{"seq":%d,"sentAt":%d,"nodes":[{"nodeId":"%s","kind":"%s","samples":[{"t":%d,"p":12.5}]}]}`,
		g.seq, time.Now().Unix(), node, kind, time.Now().Unix())

	if r := g.e.CallWith(t, http.MethodPost, "/device/v1/readings", body, map[string]string{"Authorization": "Bearer " + g.key}); r.Status != http.StatusOK {
		t.Fatalf("readings answered %d %v", r.Status, r.Body)
	}
}

func (g *rig) start(t *testing.T, kind string) testutil.Reply {
	t.Helper()

	return g.e.Call(t, http.MethodPost, "/v1/pairings", `{"kind":"`+kind+`","name":"Mesin jahit 2","roomId":"`+g.room+`"}`, g.session)
}

func (g *rig) status(t *testing.T, id string) testutil.Reply {
	t.Helper()

	return g.e.Call(t, http.MethodGet, "/v1/pairings/"+id, "", g.session)
}

func TestPairingFindsTheNodeThatTheGatewayHears(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "a")

	r := g.start(t, "plug")
	if r.Status != http.StatusCreated {
		t.Fatalf("start answered %d %v", r.Status, r.Body)
	}

	id, _ := r.Body["id"].(string)

	if s := g.status(t, id); s.Status != http.StatusOK || s.Body["status"] != "searching" || s.Body["deviceId"] != nil {
		t.Fatalf("before any node it answered %d %v", s.Status, s.Body)
	}

	// a clamp is not the kind that the search wants
	g.hear(t, "CLAMP-0001", "clamp")

	if s := g.status(t, id); s.Body["status"] != "searching" {
		t.Fatalf("a clamp ended a search for a plug %v", s.Body)
	}

	g.hear(t, "PLUG-0001", "plug")

	s := g.status(t, id)
	device, _ := s.Body["deviceId"].(string)

	if s.Body["status"] != "connected" || device == "" {
		t.Fatalf("after the plug it answered %v", s.Body)
	}

	// the answer stays the same, and the node is a device of this business only
	if again := g.status(t, id); again.Body["status"] != "connected" || again.Body["deviceId"] != device {
		t.Fatalf("a second read answered %v", again.Body)
	}

	var name, kind string
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT name, kind FROM devices WHERE id = $1", device).Scan(&name, &kind); err != nil || name != "Mesin jahit 2" || kind != "plug" {
		t.Fatalf("the device is %q %q (%v)", name, kind, err)
	}
}

func TestANodeBecomesOneDeviceOnly(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "b")
	g.hear(t, "PLUG-0001", "plug")

	first, _ := g.start(t, "plug").Body["id"].(string)
	if s := g.status(t, first); s.Body["status"] != "connected" {
		t.Fatalf("the first search answered %v", s.Body)
	}

	// the same node cannot be claimed again, so the second search keeps looking
	second, _ := g.start(t, "plug").Body["id"].(string)
	if s := g.status(t, second); s.Body["status"] != "searching" {
		t.Fatalf("the second search took a node that is a device already %v", s.Body)
	}

	var n int
	if err := g.e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM devices").Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d devices exist (%v), want 1", n, err)
	}
}

func TestOneSearchAtATimeAndItCanBeCancelled(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "c")

	id, _ := g.start(t, "plug").Body["id"].(string)

	if r := g.start(t, "clamp"); r.Status != http.StatusConflict || r.Code() != "pairing_running" {
		t.Fatalf("a second search answered %d %v", r.Status, r.Body)
	}

	if r := g.e.Call(t, http.MethodDelete, "/v1/pairings/"+id, "", g.session); r.Status != http.StatusNoContent {
		t.Fatalf("cancel answered %d", r.Status)
	}

	if s := g.status(t, id); s.Body["status"] != "cancelled" {
		t.Fatalf("after the cancel it answered %v", s.Body)
	}

	// cancelling again, or after the end, is fine and changes nothing
	if r := g.e.Call(t, http.MethodDelete, "/v1/pairings/"+id, "", g.session); r.Status != http.StatusNoContent {
		t.Fatalf("a second cancel answered %d", r.Status)
	}

	// a cancelled search does not take a node, and the owner can start another one
	g.hear(t, "PLUG-0001", "plug")

	if s := g.status(t, id); s.Body["status"] != "cancelled" {
		t.Fatalf("a cancelled search changed to %v", s.Body)
	}

	if r := g.start(t, "plug"); r.Status != http.StatusCreated {
		t.Fatalf("a new search after the cancel answered %d %v", r.Status, r.Body)
	}
}

func TestASearchThatRunsOutOfTimeFails(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "d")

	id, _ := g.start(t, "plug").Body["id"].(string)
	g.e.Exec(t, "UPDATE pairings SET expires_at = now() - interval '1 second'")

	if s := g.status(t, id); s.Body["status"] != "failed" {
		t.Fatalf("an old search answered %v", s.Body)
	}

	if r := g.start(t, "plug"); r.Status != http.StatusCreated {
		t.Fatalf("a search after a failed one answered %d %v", r.Status, r.Body)
	}
}

func TestANodeThatWentQuietBeforeTheSearchIsLeftAlone(t *testing.T) {
	g := business(t, testutil.New(t), "budi@example.com", "ESP32-AABBCC", "e")
	g.hear(t, "PLUG-0001", "plug")
	g.e.Exec(t, "UPDATE nodes SET last_seen_at = now() - interval '1 hour', first_seen_at = now() - interval '1 hour'")

	id, _ := g.start(t, "plug").Body["id"].(string)

	if s := g.status(t, id); s.Body["status"] != "searching" {
		t.Fatalf("a forgotten node became the device %v", s.Body)
	}
}

func TestStartNeedsAGatewayAndAValidBody(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")
	room, _ := e.Call(t, http.MethodPost, "/v1/rooms", `{"name":"Gudang"}`, session).Body["id"].(string)

	if r := e.Call(t, http.MethodPost, "/v1/pairings", `{"kind":"plug","name":"A","roomId":"`+room+`"}`, session); r.Status != http.StatusConflict || r.Code() != "no_gateway" {
		t.Fatalf("no gateway answered %d %v", r.Status, r.Body)
	}

	g := business(t, e, "ani@example.com", "ESP32-DDEEFF", "f")

	tests := map[string]string{
		"a bad kind":          `{"kind":"toaster","name":"A","roomId":"` + g.room + `"}`,
		"an empty name":       `{"kind":"plug","name":"  ","roomId":"` + g.room + `"}`,
		"a long name":         `{"kind":"plug","name":"` + strings.Repeat("a", 41) + `","roomId":"` + g.room + `"}`,
		"a bad room id":       `{"kind":"plug","name":"A","roomId":"gudang"}`,
		"the room of another": `{"kind":"plug","name":"A","roomId":"` + room + `"}`,
	}

	for name, body := range tests {
		if r := e.Call(t, http.MethodPost, "/v1/pairings", body, g.session); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s answered %d %v, want 422", name, r.Status, r.Body)
		}
	}
}

func TestAnotherBusinessCannotSeeOrTakeASearch(t *testing.T) {
	e := testutil.New(t)
	budi := business(t, e, "budi@example.com", "ESP32-AABBCC", "g")
	ani := business(t, e, "ani@example.com", "ESP32-DDEEFF", "h")

	id, _ := budi.start(t, "plug").Body["id"].(string)

	if r := e.Call(t, http.MethodGet, "/v1/pairings/"+id, "", ani.session); r.Status != http.StatusNotFound {
		t.Fatalf("another business read the search and got %d", r.Status)
	}

	if r := e.Call(t, http.MethodDelete, "/v1/pairings/"+id, "", ani.session); r.Status != http.StatusNotFound {
		t.Fatalf("another business cancelled the search and got %d", r.Status)
	}

	// a node that the gateway of Ani hears never becomes a device of Budi
	ani.hear(t, "PLUG-0009", "plug")

	if s := budi.status(t, id); s.Body["status"] != "searching" {
		t.Fatalf("a node of another business was taken %v", s.Body)
	}

	if r := e.Call(t, http.MethodGet, "/v1/pairings/not-an-id", "", budi.session); r.Status != http.StatusNotFound {
		t.Fatalf("a bad id answered %d", r.Status)
	}
}
