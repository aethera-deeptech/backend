package relay_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/relay"
	"github.com/aethera-deeptech/backend/internal/testutil"
)

// rig is a business with one plug on a gateway, and the gateway key is the hardware id, which the seed hashes
type rig struct {
	e   *testutil.Env
	g   *testutil.Seeded
	key string
	seq int
}

func newRig(t *testing.T) *rig {
	t.Helper()

	key := strings.Repeat("k", 40)

	e := testutil.New(t)

	return &rig{e: e, g: e.SeedDevice(t, "budi@example.com", key), key: key}
}

// batch sends one batch of the plug with an optional state of the relay and the results of the gateway, and returns the commands of
// the answer
func (r *rig) batch(t *testing.T, relayState *int, results string) []map[string]any {
	t.Helper()

	r.seq++
	now := time.Now().Unix() - int64(r.seq)*10
	extra := ""

	if relayState != nil {
		extra = fmt.Sprintf(`,"r":%d`, *relayState)
	}

	if results == "" {
		results = "[]"
	}

	body := fmt.Sprintf(`{"seq":%d,"sentAt":%d,"nodes":[{"nodeId":"PLUG-0001","kind":"plug","samples":[{"t":%d,"p":42%s}]}],"results":%s}`,
		r.seq, time.Now().Unix(), now, extra, results)

	reply := r.e.CallWith(t, http.MethodPost, "/device/v1/readings", body, map[string]string{"Authorization": "Bearer " + r.key})
	if reply.Status != http.StatusOK {
		t.Fatalf("readings answered %d %v", reply.Status, reply.Body)
	}

	list, ok := reply.Body["commands"].([]any)
	if !ok {
		t.Fatalf("the answer has no commands list %v", reply.Body)
	}

	out := make([]map[string]any, 0, len(list))

	for _, c := range list {
		m, _ := c.(map[string]any)
		out = append(out, m)
	}

	return out
}

func (r *rig) relayView(t *testing.T) map[string]any {
	t.Helper()

	reply := r.e.Call(t, http.MethodGet, "/v1/devices/"+r.g.Device+"/relay", "", r.g.Session)
	if reply.Status != http.StatusOK {
		t.Fatalf("the relay answered %d %v", reply.Status, reply.Body)
	}

	return reply.Body
}

func (r *rig) command(t *testing.T, state string) testutil.Reply {
	t.Helper()

	return r.e.Call(t, http.MethodPost, "/v1/devices/"+r.g.Device+"/relay", `{"state":"`+state+`"}`, r.g.Session)
}

func TestACommandReachesTheGatewayAndTheResultEndsIt(t *testing.T) {
	r := newRig(t)

	if v := r.relayView(t); v["state"] != "unknown" || v["pending"] != nil || v["last"] != nil || v["schedule"] != nil {
		t.Fatalf("a new plug shows %v", v)
	}

	sent := r.command(t, "off")
	id, _ := sent.Body["id"].(string)

	if sent.Status != http.StatusAccepted || sent.Body["status"] != "pending" || sent.Body["action"] != "off" || id == "" {
		t.Fatalf("the command answered %d %v", sent.Status, sent.Body)
	}

	// the gateway fetches it with its next batch, and it comes again until the gateway reports a result
	for range 2 {
		commands := r.batch(t, nil, "")
		if len(commands) != 1 || commands[0]["id"] != id || commands[0]["action"] != "off" || commands[0]["nodeId"] != "PLUG-0001" {
			t.Fatalf("the commands are %v", commands)
		}
	}

	if v := r.relayView(t); v["pending"] == nil || v["pending"].(map[string]any)["status"] != "sent" {
		t.Fatalf("the view after the fetch is %v", v)
	}

	zero := 0
	if commands := r.batch(t, &zero, fmt.Sprintf(`[{"id":"%s","ok":true}]`, id)); len(commands) != 0 {
		t.Fatalf("a command that ended came again %v", commands)
	}

	v := r.relayView(t)
	last, _ := v["last"].(map[string]any)

	if v["state"] != "off" || v["pending"] != nil || v["reportedAt"] == nil || last["status"] != "applied" || last["action"] != "off" || last["source"] != "owner" {
		t.Fatalf("the view after the result is %v", v)
	}
}

func TestAFailedResultAndAStateFromASampleAreStored(t *testing.T) {
	r := newRig(t)

	id, _ := r.command(t, "on").Body["id"].(string)
	r.batch(t, nil, "")
	r.batch(t, nil, fmt.Sprintf(`[{"id":"%s","ok":false}]`, id))

	if last, _ := r.relayView(t)["last"].(map[string]any); last["status"] != "failed" {
		t.Fatalf("the last command is %v", last)
	}

	// the relay is on again by hand, and the plug says so in a sample
	one := 1
	r.batch(t, &one, "")

	if v := r.relayView(t); v["state"] != "on" {
		t.Fatalf("the state is %v", v["state"])
	}
}

func TestANewerCommandReplacesTheOneThatWaits(t *testing.T) {
	r := newRig(t)

	r.command(t, "off")
	second, _ := r.command(t, "on").Body["id"].(string)

	commands := r.batch(t, nil, "")
	if len(commands) != 1 || commands[0]["id"] != second || commands[0]["action"] != "on" {
		t.Fatalf("the commands are %v", commands)
	}
}

func TestACommandThatWaitedTooLongIsNotSent(t *testing.T) {
	r := newRig(t)

	r.command(t, "off")
	r.e.Exec(t, "UPDATE relay_commands SET expires_at = now() - interval '1 minute'")

	if commands := r.batch(t, nil, ""); len(commands) != 0 {
		t.Fatalf("an old command was sent %v", commands)
	}

	if n := (relay.NewScheduler(r.e.Pool, time.Minute)).Round(t.Context()); n != 0 {
		t.Fatalf("a round made %d commands with no schedule", n)
	}

	var status string
	if err := r.e.Pool.QueryRow(t.Context(), "SELECT status FROM relay_commands").Scan(&status); err != nil || status != "expired" {
		t.Fatalf("the command is %q (%v)", status, err)
	}
}

func TestAResultForAnotherCommandIsIgnored(t *testing.T) {
	r := newRig(t)

	r.command(t, "off")
	r.batch(t, nil, `[{"id":"not-a-number","ok":true},{"id":"00000000-0000-4000-8000-000000000000","ok":true}]`)

	if v := r.relayView(t); v["pending"] == nil {
		t.Fatalf("a stray result ended the command %v", v)
	}
}

func TestOnlyAPlugOfTheBusinessHasARelay(t *testing.T) {
	r := newRig(t)

	if bad := r.command(t, "toggle"); bad.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a bad state answered %d", bad.Status)
	}

	for _, path := range []string{"/v1/devices/not-a-number/relay", "/v1/devices/00000000-0000-4000-8000-000000000000/relay"} {
		if got := r.e.Call(t, http.MethodGet, path, "", r.g.Session); got.Status != http.StatusNotFound {
			t.Fatalf("%s answered %d", path, got.Status)
		}
	}

	other := r.e.SignUp(t, "siti@example.com")
	if got := r.e.Call(t, http.MethodPost, "/v1/devices/"+r.g.Device+"/relay", `{"state":"off"}`, other); got.Status != http.StatusNotFound {
		t.Fatalf("a device of another business answered %d", got.Status)
	}

	if got := r.e.Call(t, http.MethodPost, "/v1/devices/"+r.g.Device+"/relay", `{"state":"off"}`, ""); got.Status != http.StatusUnauthorized {
		t.Fatalf("no session answered %d", got.Status)
	}

	r.e.Exec(t, "UPDATE devices SET kind = 'clamp'")
	r.e.Exec(t, "UPDATE nodes SET kind = 'clamp'")

	if got := r.command(t, "off"); got.Status != http.StatusConflict || got.Body["code"] != "no_relay" {
		t.Fatalf("a clamp answered %d %v", got.Status, got.Body)
	}
}

func TestTheOwnerSetsAndRemovesASchedule(t *testing.T) {
	r := newRig(t)
	path := "/v1/devices/" + r.g.Device + "/schedule"

	for _, bad := range []string{
		`{"offAt":"25:00","days":"daily"}`,
		`{"offAt":"17:00","onAt":"17:00","days":"daily"}`,
		`{"offAt":"17:00","onAt":"8:00","days":"daily"}`,
		`{"offAt":"17:00","days":"weekends"}`,
		`{"days":"daily"}`,
	} {
		if got := r.e.Call(t, http.MethodPut, path, bad, r.g.Session); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s answered %d", bad, got.Status)
		}
	}

	put := r.e.Call(t, http.MethodPut, path, `{"offAt":"17:30","onAt":"08:00","days":"mon-fri"}`, r.g.Session)
	if put.Status != http.StatusOK || put.Body["offAt"] != "17:30" || put.Body["onAt"] != "08:00" || put.Body["enabled"] != true {
		t.Fatalf("the schedule answered %d %v", put.Status, put.Body)
	}

	if s, _ := r.relayView(t)["schedule"].(map[string]any); s["offAt"] != "17:30" || s["days"] != "mon-fri" {
		t.Fatalf("the view holds %v", s)
	}

	// the schedule can be paused, and it can have no time to switch on
	r.e.Call(t, http.MethodPut, path, `{"offAt":"17:30","days":"daily","enabled":false}`, r.g.Session)

	if s, _ := r.relayView(t)["schedule"].(map[string]any); s["enabled"] != false || s["onAt"] != nil {
		t.Fatalf("the paused schedule is %v", s)
	}

	for range 2 {
		if got := r.e.Call(t, http.MethodDelete, path, "", r.g.Session); got.Status != http.StatusNoContent {
			t.Fatalf("delete answered %d", got.Status)
		}
	}

	if v := r.relayView(t); v["schedule"] != nil {
		t.Fatalf("the schedule is still there %v", v["schedule"])
	}
}

func TestAScheduleMakesACommandWhenItsTimePasses(t *testing.T) {
	r := newRig(t)
	zone := time.FixedZone("WIB", 7*3600)

	// the business is in Asia/Jakarta, and the switch time was a minute ago, after the last look 5 minutes ago
	at := time.Now().In(zone).Add(-time.Minute).Format("15:04")
	put := r.e.Call(t, http.MethodPut, "/v1/devices/"+r.g.Device+"/schedule", fmt.Sprintf(`{"offAt":"%s","days":"daily"}`, at), r.g.Session)

	if put.Status != http.StatusOK {
		t.Fatalf("the schedule answered %d %v", put.Status, put.Body)
	}

	// a schedule never acts on a time before it was set
	scheduler := relay.NewScheduler(r.e.Pool, time.Minute)
	if n := scheduler.Round(t.Context()); n != 0 {
		t.Fatalf("a new schedule made %d commands", n)
	}

	r.e.Exec(t, "UPDATE relay_schedules SET checked_at = now() - interval '5 minutes'")

	if n := scheduler.Round(t.Context()); n != 1 {
		t.Fatalf("the round made %d commands, want 1", n)
	}

	// the same time is not used twice
	if n := scheduler.Round(t.Context()); n != 0 {
		t.Fatalf("the second round made %d commands", n)
	}

	commands := r.batch(t, nil, "")
	if len(commands) != 1 || commands[0]["action"] != "off" {
		t.Fatalf("the commands are %v", commands)
	}

	if v := r.relayView(t); v["pending"].(map[string]any)["action"] != "off" {
		t.Fatalf("the view is %v", v)
	}

	var source string
	if err := r.e.Pool.QueryRow(t.Context(), "SELECT source FROM relay_commands WHERE status = 'sent'").Scan(&source); err != nil || source != "schedule" {
		t.Fatalf("the source is %q (%v)", source, err)
	}
}

func TestAResultForTheCommandOfAnotherGatewayIsIgnored(t *testing.T) {
	r := newRig(t)

	// a second business with its own gateway, plug and command
	other := r.e.SeedDevice(t, "siti@example.com", strings.Repeat("j", 40))
	foreign, _ := r.e.Call(t, http.MethodPost, "/v1/devices/"+other.Device+"/relay", `{"state":"off"}`, other.Session).Body["id"].(string)

	own, _ := r.command(t, "on").Body["id"].(string)

	r.batch(t, nil, fmt.Sprintf(`[{"id":"%s","ok":true}]`, foreign))

	var status string
	if err := r.e.Pool.QueryRow(t.Context(), "SELECT status FROM relay_commands WHERE id = $1", foreign).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("the foreign command is %q (%v), a gateway ended the command of another business", status, err)
	}

	// the own command is still open, and the foreign result changed nothing of this plug
	if v := r.relayView(t); v["pending"] == nil || v["pending"].(map[string]any)["id"] != own || v["state"] != "unknown" {
		t.Fatalf("the view is %v", v)
	}
}

func TestAResultMovesTheStateWithoutASampleAndAReplacedCommandCannotEndItsSuccessor(t *testing.T) {
	r := newRig(t)

	first, _ := r.command(t, "off").Body["id"].(string)
	second, _ := r.command(t, "on").Body["id"].(string)

	// the result of the replaced command changes nothing
	r.batch(t, nil, fmt.Sprintf(`[{"id":"%s","ok":true}]`, first))

	if v := r.relayView(t); v["state"] != "unknown" || v["pending"] == nil {
		t.Fatalf("the replaced command changed the plug %v", v)
	}

	// the result of the open command sets the state, with no sample that says so
	r.batch(t, nil, fmt.Sprintf(`[{"id":"%s","ok":true}]`, second))

	if v := r.relayView(t); v["state"] != "on" || v["pending"] != nil {
		t.Fatalf("the result did not move the state %v", v)
	}
}

func TestAnAnswerThatIsSentAgainCarriesTheCommandsToo(t *testing.T) {
	r := newRig(t)

	id, _ := r.command(t, "off").Body["id"].(string)

	// the same sequence number again is a retry of a batch whose answer was lost
	r.seq++
	body := fmt.Sprintf(`{"seq":%d,"sentAt":%d,"nodes":[{"nodeId":"PLUG-0001","kind":"plug","samples":[{"t":%d,"p":42}]}]}`, r.seq, time.Now().Unix(), time.Now().Unix()-30)

	for range 2 {
		reply := r.e.CallWith(t, http.MethodPost, "/device/v1/readings", body, map[string]string{"Authorization": "Bearer " + r.key})
		list, _ := reply.Body["commands"].([]any)

		if reply.Status != http.StatusOK || len(list) != 1 || list[0].(map[string]any)["id"] != id {
			t.Fatalf("the answer is %d %v", reply.Status, reply.Body)
		}
	}
}

func TestADeviceHasOnlyOneOpenCommand(t *testing.T) {
	r := newRig(t)

	for _, state := range []string{"off", "on", "off"} {
		r.command(t, state)
	}

	var open int
	if err := r.e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM relay_commands WHERE status IN ('pending', 'sent')").Scan(&open); err != nil || open != 1 {
		t.Fatalf("there are %d open commands (%v)", open, err)
	}

	// the database refuses a second open command, whatever the code does
	if _, err := r.e.Pool.Exec(t.Context(),
		"INSERT INTO relay_commands (device_id, gateway_id, node_id, action, source, expires_at) SELECT device_id, gateway_id, node_id, 'on', 'owner', now() + interval '1 minute' FROM relay_commands LIMIT 1"); err == nil {
		t.Fatal("the database took a second open command")
	}
}

func TestTooManyResultsAreRefused(t *testing.T) {
	r := newRig(t)

	list := make([]string, 65)
	for i := range list {
		list[i] = `{"id":"00000000-0000-4000-8000-000000000000","ok":true}`
	}

	body := fmt.Sprintf(`{"seq":1,"sentAt":%d,"nodes":[{"nodeId":"PLUG-0001","kind":"plug","samples":[{"t":%d,"p":42}]}],"results":[%s]}`, time.Now().Unix(), time.Now().Unix()-30, strings.Join(list, ","))

	if got := r.e.CallWith(t, http.MethodPost, "/device/v1/readings", body, map[string]string{"Authorization": "Bearer " + r.key}); got.Status != http.StatusUnprocessableEntity {
		t.Fatalf("65 results answered %d", got.Status)
	}
}

func TestAnIdInUrnFormIsIgnoredAndDoesNotBreakTheBatch(t *testing.T) {
	r := newRig(t)

	r.command(t, "off")

	// the urn form parses as a uuid, but PostgreSQL does not take it, so it must not reach a query
	r.batch(t, nil, `[{"id":"urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8","ok":true}]`)

	if got := r.e.Call(t, http.MethodGet, "/v1/devices/urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8/relay", "", r.g.Session); got.Status != http.StatusNotFound {
		t.Fatalf("a device id in urn form answered %d", got.Status)
	}

	if v := r.relayView(t); v["pending"] == nil {
		t.Fatalf("the batch lost the command %v", v)
	}
}

func TestARealDeviceAddressedInUrnFormWorksOnEveryRoute(t *testing.T) {
	r := newRig(t)
	base := "/v1/devices/urn:uuid:" + r.g.Device

	if got := r.e.Call(t, http.MethodPost, base+"/relay", `{"state":"off"}`, r.g.Session); got.Status != http.StatusAccepted {
		t.Fatalf("the command answered %d %v", got.Status, got.Body)
	}

	if got := r.e.Call(t, http.MethodGet, base+"/relay", "", r.g.Session); got.Status != http.StatusOK || got.Body["pending"] == nil {
		t.Fatalf("the relay answered %d %v", got.Status, got.Body)
	}

	if got := r.e.Call(t, http.MethodPut, base+"/schedule", `{"offAt":"17:00","days":"daily"}`, r.g.Session); got.Status != http.StatusOK {
		t.Fatalf("the schedule answered %d %v", got.Status, got.Body)
	}

	if got := r.e.Call(t, http.MethodDelete, base+"/schedule", "", r.g.Session); got.Status != http.StatusNoContent {
		t.Fatalf("delete answered %d", got.Status)
	}
}

// hourlyCommands is the limit of commands for a business in an hour, as ruleCommand sets it
const hourlyCommands = 60

// TestOnlyAQueuedCommandUsesTheLimitOfTheBusiness checks that a call that ends in 404 or 409 does not use up the 60 commands of an
// hour, and that the 61st queued command is refused with the time to wait
func TestOnlyAQueuedCommandUsesTheLimitOfTheBusiness(t *testing.T) {
	r := newRig(t)

	for range hourlyCommands + 5 {
		if got := r.e.Call(t, http.MethodPost, "/v1/devices/00000000-0000-4000-8000-000000000000/relay", `{"state":"off"}`, r.g.Session); got.Status != http.StatusNotFound {
			t.Fatalf("an unknown device answered %d", got.Status)
		}
	}

	r.e.Exec(t, "UPDATE devices SET kind = 'clamp'")
	r.e.Exec(t, "UPDATE nodes SET kind = 'clamp'")

	for range hourlyCommands + 5 {
		if got := r.command(t, "off"); got.Status != http.StatusConflict {
			t.Fatalf("a clamp answered %d", got.Status)
		}
	}

	r.e.Exec(t, "UPDATE devices SET kind = 'plug'")
	r.e.Exec(t, "UPDATE nodes SET kind = 'plug'")

	for i := range hourlyCommands {
		if got := r.command(t, "off"); got.Status != http.StatusAccepted {
			t.Fatalf("command %d answered %d %v", i+1, got.Status, got.Body)
		}
	}

	got := r.command(t, "on")
	if got.Status != http.StatusTooManyRequests || got.Body["code"] != "rate_limited" || got.Body["retryAfterSeconds"] == nil {
		t.Fatalf("the command over the limit answered %d %v", got.Status, got.Body)
	}
}

func TestTheRelayViewSaysWhetherTheGatewayIsOnline(t *testing.T) {
	r := newRig(t)

	// a gateway that never sent a batch is not online
	r.e.Exec(t, "UPDATE gateways SET last_seen_at = NULL")

	if got := r.relayView(t)["gatewayOnline"]; got != false {
		t.Fatalf("a gateway that never reported is online %v", got)
	}

	r.batch(t, nil, "")

	if got := r.relayView(t)["gatewayOnline"]; got != true {
		t.Fatalf("a gateway that just sent a batch is not online %v", got)
	}

	r.e.Exec(t, "UPDATE gateways SET last_seen_at = now() - interval '20 minutes'")

	if got := r.relayView(t)["gatewayOnline"]; got != false {
		t.Fatalf("a gateway that was silent for 20 minutes is online %v", got)
	}

	// a command is still queued while the gateway is away
	if got := r.command(t, "off"); got.Status != http.StatusAccepted {
		t.Fatalf("a command for a silent gateway answered %d %v", got.Status, got.Body)
	}
}

// sampleAt sends one batch with one sample of the plug at the time t, with the extra fields of the sample and the results of the
// gateway. The times of one test grow, because a state that a sample reports only counts when the sample is newer than the last one.
func (r *rig) sampleAt(t *testing.T, at int64, extra, results string) {
	t.Helper()

	r.seq++

	if results == "" {
		results = "[]"
	}

	body := fmt.Sprintf(`{"seq":%d,"sentAt":%d,"nodes":[{"nodeId":"PLUG-0001","kind":"plug","samples":[{"t":%d,"p":42%s}]}],"results":%s}`,
		r.seq, time.Now().Unix(), at, extra, results)

	if reply := r.e.CallWith(t, http.MethodPost, "/device/v1/readings", body, map[string]string{"Authorization": "Bearer " + r.key}); reply.Status != http.StatusOK {
		t.Fatalf("the batch answered %d %v", reply.Status, reply.Body)
	}
}

func TestAPlugThatCutItselfShowsTheCauseUntilSomethingSwitchesItAgain(t *testing.T) {
	r := newRig(t)

	base := time.Now().Unix() - 600

	r.sampleAt(t, base+10, `,"r":1`, "")

	if v := r.relayView(t); v["state"] != "on" || v["cutByDevice"] != nil {
		t.Fatalf("a plug that is on shows %v", v)
	}

	// the plug cuts its own relay and reports the code of an overload
	r.sampleAt(t, base+20, `,"a":3,"r":0`, "")

	v := r.relayView(t)
	cut, _ := v["cutByDevice"].(map[string]any)

	if v["state"] != "off" || cut["reason"] != "overload" || cut["at"] == nil {
		t.Fatalf("a plug that cut itself shows %v", v)
	}

	// the owner switches it on, and the cause goes with the state
	id, _ := r.command(t, "on").Body["id"].(string)

	r.batch(t, nil, "")
	r.sampleAt(t, base+30, `,"r":1`, fmt.Sprintf(`[{"id":"%s","ok":true}]`, id))

	if v := r.relayView(t); v["state"] != "on" || v["cutByDevice"] != nil {
		t.Fatalf("a plug that the owner switched on shows %v", v)
	}

	// a plug that is off because of a command that came after the overload has no cause from the device
	offID, _ := r.command(t, "off").Body["id"].(string)

	r.batch(t, nil, "")
	r.sampleAt(t, base+40, `,"r":0`, fmt.Sprintf(`[{"id":"%s","ok":true}]`, offID))

	if v := r.relayView(t); v["state"] != "off" || v["cutByDevice"] != nil {
		t.Fatalf("a plug that the owner switched off shows %v", v)
	}

	// the commands are older than the overload now, so the overload is the cause again, until it is more than a day old
	r.e.Exec(t, "UPDATE relay_commands SET done_at = now() - interval '3 days', created_at = now() - interval '3 days'")

	if v := r.relayView(t); v["cutByDevice"] == nil {
		t.Fatalf("an overload that came after the last command is not shown %v", v)
	}

	r.e.Exec(t, "UPDATE anomalies SET at = now() - interval '25 hours'")

	if v := r.relayView(t); v["cutByDevice"] != nil {
		t.Fatalf("an old overload is shown as the cause %v", v)
	}
}
