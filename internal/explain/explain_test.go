//go:build explain

// Package explain seeds a throwaway database with a business that has many devices and a long history, calls every read route of the
// app and the intake of readings, and writes the EXPLAIN plans of the statements that those calls ran. It never runs in the normal
// test job, because of the build tag, and a call that is slower than the cap fails the test, so a query that grows with the data is
// found before it reaches production. The database is the throwaway server of the TEST_DB_ variables and never a real one.
package explain

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/database"
	"github.com/aethera-deeptech/backend/internal/database/dbtest"
	"github.com/aethera-deeptech/backend/internal/janitor"
	"github.com/aethera-deeptech/backend/internal/relay"
	"github.com/aethera-deeptech/backend/internal/testutil"
)

// recorder keeps every distinct statement that the pool runs while it is switched on, with the first arguments that it ran with
type recorder struct {
	mu   sync.Mutex
	on   bool
	seen map[string]bool
	list []statement
}

type statement struct {
	sql  string
	args []any
}

func (r *recorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.on && !r.seen[data.SQL] {
		r.seen[data.SQL] = true
		r.list = append(r.list, statement{sql: strings.Clone(data.SQL), args: copyArgs(data.Args)})
	}

	return ctx
}

// copyArgs copies the arguments of a statement. Fiber hands out the strings of a request as views of buffers that the next request
// writes over, and the plans are made after every call, so a string or a byte slice that is kept as it is would hold the wrong text.
func copyArgs(args []any) []any {
	out := make([]any, len(args))

	for i, a := range args {
		switch v := a.(type) {
		case string:
			out[i] = strings.Clone(v)
		case []byte:
			out[i] = bytes.Clone(v)
		default:
			out[i] = a
		}
	}

	return out
}

func (*recorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func number(name string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}

	return fallback
}

func TestExplainThePlansOfTheQueriesToWatch(t *testing.T) {
	devices := number("EXPLAIN_DEVICES", 100)
	days := number("EXPLAIN_DAYS", 400)
	capSeconds := number("EXPLAIN_QUERY_TIMEOUT_SECONDS", 10)
	out := os.Getenv("EXPLAIN_OUT")

	// a call may take as long as the cap, and a little more so that the cap is what fails and not the timeout of the request
	testutil.RequestTimeout = time.Duration(capSeconds+5) * time.Second

	dbURL := dbtest.Empty(t)
	if err := database.Migrate(dbURL); err != nil {
		t.Fatalf("migrate %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatal(err)
	}

	rec := &recorder{seen: map[string]bool{}}
	cfg.ConnConfig.Tracer = rec
	// the settings of the pool of the service, so the plans are made the way production makes them
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
	cfg.MaxConns = 8

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	e := testutil.NewOn(t, pool)
	session := e.SignUp(t, "explain@example.com")

	var business string
	if err := pool.QueryRow(t.Context(), "SELECT business_id::text FROM users WHERE email = 'explain@example.com'").Scan(&business); err != nil {
		t.Fatal(err)
	}

	key := strings.Repeat("a", 43)
	// the seed runs on a connection with no time limit, because the pool has the statement timeout of production
	plain, err := pgx.Connect(t.Context(), dbURL)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = plain.Close(context.Background()) }()

	seed(t, plain, business, key, devices, days)

	rec.mu.Lock()
	rec.on = true
	rec.mu.Unlock()

	var device string
	if err := pool.QueryRow(t.Context(), "SELECT id::text FROM devices WHERE business_id = $1 ORDER BY name LIMIT 1", business).Scan(&device); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	batch := fmt.Sprintf(`{"seq":1,"sentAt":%d,"nodes":[{"nodeId":"NODE-0001","kind":"plug","samples":[{"t":%d,"p":150.5},{"t":%d,"p":160.5}]}]}`, now, now-20, now-10)

	// the second batch reports the result of a command that waited for the gateway, so the statements that store a result run too
	var pending string
	if err := plain.QueryRow(t.Context(), "SELECT id::text FROM relay_commands WHERE status = 'pending' ORDER BY created_at LIMIT 1").Scan(&pending); err != nil {
		t.Fatal(err)
	}

	batchWithResult := fmt.Sprintf(`{"seq":2,"sentAt":%d,"nodes":[{"nodeId":"NODE-0001","kind":"plug","samples":[{"t":%d,"p":155.5,"r":1}]}],"results":[{"id":%q,"ok":true}]}`, now+10, now, pending)

	period := time.Now().Format("2006-01")
	older := time.Now().AddDate(0, -2, 0).Format("2006-01")

	calls := []struct {
		name, method, path, body string
		bearer                   bool
		want                     int
	}{
		{"list the devices", http.MethodGet, "/v1/devices", "", false, 200},
		{"read a device", http.MethodGet, "/v1/devices/" + device, "", false, 200},
		{"the home screen", http.MethodGet, "/v1/beranda", "", false, 200},
		{"the tips", http.MethodGet, "/v1/recommendations", "", false, 200},
		{"the production list", http.MethodGet, "/v1/production", "", false, 200},
		{"the report of this month", http.MethodGet, "/v1/reports?period=" + period, "", false, 200},
		{"the report of an older month", http.MethodGet, "/v1/reports?period=" + older, "", false, 200},
		{"the PDF of the report", http.MethodGet, "/v1/reports/" + period + "/pdf", "", false, 200},
		{"the intake of a batch", http.MethodPost, "/device/v1/readings", batch, true, 200},
		{"the intake of a batch with a relay result", http.MethodPost, "/device/v1/readings", batchWithResult, true, 200},
		{"the relay of a device", http.MethodGet, "/v1/devices/" + device + "/relay", "", false, 200},
		{"a relay command of the owner", http.MethodPost, "/v1/devices/" + device + "/relay", `{"state":"off"}`, false, 202},
		{"a schedule of a device", http.MethodPut, "/v1/devices/" + device + "/schedule", `{"offAt":"17:30","onAt":"08:00","days":"mon-sat"}`, false, 200},
		{"the choice after a power loss", http.MethodPut, "/v1/devices/" + device + "/power-loss", `{"afterPowerLoss":"last"}`, false, 200},
	}

	for _, c := range calls {
		// the tips are worked out again on every read, so the plans are those of the refresh and not of the cached path
		e.Exec(t, "UPDATE businesses SET tips_refreshed_at = NULL")

		started := time.Now()

		var r testutil.Reply
		if c.bearer {
			r = e.CallWith(t, c.method, c.path, c.body, map[string]string{"Authorization": "Bearer " + key})
		} else {
			r = e.Call(t, c.method, c.path, c.body, session)
		}

		took := time.Since(started)
		t.Logf("%s answered %d in %s", c.name, r.Status, took.Round(time.Millisecond))

		if c.want != 0 && r.Status != c.want {
			t.Errorf("%s answered %d %s", c.name, r.Status, r.Raw)
		}

		if took > time.Duration(capSeconds)*time.Second {
			t.Errorf("%s took %s, the cap is %d seconds", c.name, took, capSeconds)
		}
	}

	// the rounds of the background tasks that work on the relay tables, the scheduler that expires and makes commands and the purge of
	// old commands, run while the recorder is on, and a round that is slower than the cap fails like a call
	rounds := []struct {
		name string
		run  func() int64
	}{
		{"a round of the relay scheduler", func() int64 { return int64(relay.NewScheduler(pool, time.Minute).Round(t.Context())) }},
		{"a round of the relay purge", func() int64 { return janitor.New(pool, time.Hour, relay.PurgeTasks()...).Round(t.Context()) }},
	}

	for _, r := range rounds {
		started := time.Now()

		did := r.run()

		took := time.Since(started)
		t.Logf("%s took %s and did %d", r.name, took.Round(time.Millisecond), did)

		// a round returns the number of commands made or of rows deleted and logs its failures, so a count of zero means that the
		// seed no longer gives the round any work and the statements of it are not in the plans
		if did == 0 {
			t.Errorf("%s did nothing, so its statements are not watched", r.name)
		}

		if took > time.Duration(capSeconds)*time.Second {
			t.Errorf("%s took %s, the cap is %d seconds", r.name, took, capSeconds)
		}
	}

	rec.mu.Lock()
	rec.on = false
	list := rec.list
	rec.mu.Unlock()

	plans(t, pool, list, out)
}

// seed writes one business with the number of devices and the days of hourly sums, about 30 days of raw readings every 10 minutes,
// and a production record for every day
func seed(t *testing.T, conn *pgx.Conn, business, key string, devices, days int) {
	t.Helper()

	ctx := t.Context()

	steps := []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO gateways (business_id, name, hardware_id, key_hash) VALUES ($1, 'Hub', 'EXPLAIN-HUB-1', sha256(convert_to($2, 'UTF8')))", []any{business, key}},
		{"INSERT INTO rooms (business_id, name) VALUES ($1, 'Ruang jahit')", []any{business}},
		{"INSERT INTO nodes (gateway_id, node_id, kind, last_sample_at) SELECT g.id, 'NODE-' || lpad(n::text, 4, '0'), 'plug', now() FROM gateways g, generate_series(1, $2::int) n WHERE g.business_id = $1", []any{business, devices}},
		{"INSERT INTO devices (business_id, gateway_id, node_id, kind, name, room_id) " +
			"SELECT $1, g.id, 'NODE-' || lpad(n::text, 4, '0'), 'plug', 'Mesin ' || n, (SELECT id FROM rooms WHERE business_id = $1 LIMIT 1) FROM gateways g, generate_series(1, $2::int) n WHERE g.business_id = $1", []any{business, devices}},
		{"INSERT INTO readings_hourly (gateway_id, node_id, hour, samples, sum_watts, max_watts, min_watts, energy_kwh) " +
			"SELECT n.gateway_id, n.node_id, date_trunc('hour', now()) - make_interval(hours => h), 60, 60 * w, w + 30, 5, w / 1000.0 " +
			"FROM nodes n, generate_series(1, $1::int * 24) h, LATERAL (SELECT 20 + (h % 24) * 7 + (length(n.node_id) % 5) AS w) x", []any{days}},
		{"INSERT INTO readings (gateway_id, node_id, t, watts, energy_kwh) " +
			"SELECT n.gateway_id, n.node_id, now() - make_interval(mins => m * 10), 20 + (m % 144), m * 0.01 " +
			"FROM nodes n, generate_series(1, 30 * 144) m", nil},
		{"INSERT INTO production_records (business_id, day, quantity, unit) SELECT $1::uuid, (now() AT TIME ZONE 'Asia/Jakarta')::date - d, 100 + d % 20, 'potong' FROM generate_series(0, $2::int) d", []any{business, days}},
		// a schedule for every device whose times passed a minute and three minutes ago in the time zone of the business, and whose last look
		// was five minutes ago, so a round of the scheduler finds a time to act on at any hour of the day. The window of the scheduler is
		// ten minutes and a time of the day before counts, so a run near midnight works as well.
		{"INSERT INTO relay_schedules (device_id, off_at, on_at, days, checked_at) " +
			"SELECT d.id, ((now() AT TIME ZONE 'Asia/Jakarta') - interval '1 minute')::time, ((now() AT TIME ZONE 'Asia/Jakarta') - interval '3 minutes')::time, 'daily', now() - interval '5 minutes' " +
			"FROM devices d WHERE d.business_id = $1", []any{business}},
		// 45 days of ended commands, two for each device a day, so the lists and the purge have rows to scan, with 15 days past the purge
		{"INSERT INTO relay_commands (device_id, gateway_id, node_id, action, source, status, created_at, expires_at, done_at) " +
			"SELECT d.id, d.gateway_id, d.node_id, CASE WHEN h % 2 = 0 THEN 'on' ELSE 'off' END, CASE WHEN h % 3 = 0 THEN 'schedule' ELSE 'owner' END, 'applied', " +
			"now() - make_interval(hours => h * 12), now() - make_interval(hours => h * 12) + interval '10 minutes', now() - make_interval(hours => h * 12) + interval '1 minute' " +
			"FROM devices d, generate_series(1, 90) h WHERE d.business_id = $1", []any{business}},
		// five commands that wait for the gateway, and three that were sent and have run out of time, which the scheduler ends
		{"INSERT INTO relay_commands (device_id, gateway_id, node_id, action, source, status, created_at, expires_at) " +
			"SELECT d.id, d.gateway_id, d.node_id, 'off', 'owner', 'pending', now() - interval '1 minute', now() + interval '9 minutes' " +
			"FROM (SELECT * FROM devices WHERE business_id = $1 ORDER BY name OFFSET 1 LIMIT 5) d", []any{business}},
		{"INSERT INTO relay_commands (device_id, gateway_id, node_id, action, source, status, created_at, expires_at, sent_at) " +
			"SELECT d.id, d.gateway_id, d.node_id, 'on', 'owner', 'sent', now() - interval '1 hour', now() - interval '50 minutes', now() - interval '55 minutes' " +
			"FROM (SELECT * FROM devices WHERE business_id = $1 ORDER BY name OFFSET 6 LIMIT 3) d", []any{business}},
		{"ANALYZE", nil},
	}

	for _, s := range steps {
		if _, err := conn.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("seed %q %v", s.sql[:min(len(s.sql), 60)], err)
		}
	}
}

// plans writes the EXPLAIN plan of every recorded statement that reads or writes a table. A statement that changes data runs inside a
// transaction that is rolled back.
func plans(t *testing.T, pool *pgxpool.Pool, list []statement, out string) {
	t.Helper()

	var b strings.Builder

	for _, s := range list {
		head := strings.ToUpper(strings.TrimSpace(s.sql))
		// the calls of the advisory locks have no plan to watch, and an EXPLAIN ANALYZE of one would take a lock on a pooled connection
		if strings.HasPrefix(head, "SELECT PG_") {
			continue
		}

		if !strings.HasPrefix(head, "SELECT") && !strings.HasPrefix(head, "WITH") && !strings.HasPrefix(head, "INSERT") &&
			!strings.HasPrefix(head, "UPDATE") && !strings.HasPrefix(head, "DELETE") {
			continue
		}

		fmt.Fprintf(&b, "==== %s\n", strings.Join(strings.Fields(s.sql), " "))

		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		// the service ends the open command of a device before it stores a new one, and the recorded INSERT runs alone here, so the open
		// commands that the calls left would break the unique index of one open command for each device. The transaction is rolled back.
		if strings.HasPrefix(head, "INSERT INTO RELAY_COMMANDS") {
			if _, err := tx.Exec(t.Context(), "UPDATE relay_commands SET status = 'expired' WHERE status IN ('pending', 'sent')"); err != nil {
				t.Fatal(err)
			}
		}

		rows, err := tx.Query(t.Context(), "EXPLAIN (ANALYZE, BUFFERS) "+s.sql, s.args...)
		if err != nil {
			fmt.Fprintf(&b, "!! EXPLAIN of this statement failed %v\n", err)
			t.Errorf("explain %q %v", s.sql[:min(len(s.sql), 80)], err)
			_ = tx.Rollback(t.Context())

			continue
		}

		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				fmt.Fprintf(&b, "!! reading the plan failed %v\n", err)
				t.Errorf("read a plan %v", err)

				break
			}

			b.WriteString(line + "\n")
		}

		if err := rows.Err(); err != nil {
			fmt.Fprintf(&b, "!! reading the plan failed %v\n", err)
			t.Errorf("read a plan %v", err)
		}

		rows.Close()
		_ = tx.Rollback(t.Context())
	}

	if out != "" {
		if err := os.WriteFile(out, []byte(b.String()), 0o600); err != nil {
			t.Errorf("write the plans %v", err)
		}
	}

	t.Logf("%d statements explained", len(list))
}
