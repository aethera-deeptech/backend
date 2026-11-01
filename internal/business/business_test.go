package business_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

func TestSettingsNeedASession(t *testing.T) {
	e := testutil.New(t)

	for _, route := range [][2]string{{http.MethodGet, "/v1/settings"}, {http.MethodPatch, "/v1/settings"}, {http.MethodGet, "/v1/rooms"}, {http.MethodPost, "/v1/rooms"}} {
		if r := e.Call(t, route[0], route[1], `{"name":"x"}`, ""); r.Status != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d without a session", route[0], route[1], r.Status)
		}
	}
}

func TestDefaultSettings(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	r := e.Call(t, http.MethodGet, "/v1/settings", "", session)
	if r.Status != http.StatusOK {
		t.Fatalf("answered %d %v", r.Status, r.Body)
	}

	tariff, _ := r.Body["tariff"].(map[string]any)
	hours, _ := r.Body["workHours"].(map[string]any)

	if r.Body["businessName"] != "Toko Maju" || r.Body["anomalySensitivity"] != "sedang" || r.Body["timeZone"] != "Asia/Jakarta" ||
		hours["days"] != "mon-sat" || hours["start"] != "08:00" || hours["end"] != "17:00" ||
		tariff["rpPerKwh"] != float64(1444) || tariff["isDefault"] != true || tariff["defaultRpPerKwh"] != float64(1444) {
		t.Fatalf("unexpected defaults %v", r.Body)
	}
}

func TestChangeSettings(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	patch := func(body string) testutil.Reply {
		return e.Call(t, http.MethodPatch, "/v1/settings", body, session)
	}

	r := patch(`{"tariffRpPerKwh":1699,"anomalySensitivity":"tinggi","businessName":"  Konveksi Baru  ","timeZone":"Asia/Makassar",
		"workHours":{"days":"daily","start":"07:30","end":"21:00"}}`)
	tariff, _ := r.Body["tariff"].(map[string]any)
	hours, _ := r.Body["workHours"].(map[string]any)

	if r.Status != http.StatusOK || tariff["rpPerKwh"] != float64(1699) || tariff["isDefault"] != false || r.Body["businessName"] != "Konveksi Baru" ||
		r.Body["anomalySensitivity"] != "tinggi" || r.Body["timeZone"] != "Asia/Makassar" || hours["days"] != "daily" || hours["start"] != "07:30" {
		t.Fatalf("patch answered %d %v", r.Status, r.Body)
	}

	// a change of one field leaves the others as they were
	if r = patch(`{"anomalySensitivity":"rendah"}`); r.Body["businessName"] != "Konveksi Baru" || r.Body["anomalySensitivity"] != "rendah" {
		t.Fatalf("a single field changed more %v", r.Body)
	}

	// null goes back to the default tariff
	r = patch(`{"tariffRpPerKwh":null}`)
	tariff, _ = r.Body["tariff"].(map[string]any)

	if r.Status != http.StatusOK || tariff["isDefault"] != true || tariff["rpPerKwh"] != float64(1444) {
		t.Fatalf("null did not reset the tariff %d %v", r.Status, r.Body)
	}

	if r = e.Call(t, http.MethodGet, "/v1/settings", "", session); r.Body["timeZone"] != "Asia/Makassar" {
		t.Fatalf("the change was not stored %v", r.Body)
	}
}

func TestSettingsValidation(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	tests := []struct {
		name, body, field, code string
		status                  int
	}{
		{"end before start", `{"workHours":{"days":"mon-fri","start":"17:00","end":"08:00"}}`, "workHours.end", "end_before_start", 422},
		{"end equals start", `{"workHours":{"days":"mon-fri","start":"08:00","end":"08:00"}}`, "workHours.end", "end_before_start", 422},
		{"bad time", `{"workHours":{"days":"mon-fri","start":"8:00","end":"17:00"}}`, "workHours.start", "invalid", 422},
		{"hour 24", `{"workHours":{"days":"mon-fri","start":"08:00","end":"24:00"}}`, "workHours.end", "invalid", 422},
		{"bad days", `{"workHours":{"days":"weekends","start":"08:00","end":"17:00"}}`, "workHours.days", "invalid", 422},
		{"tariff zero", `{"tariffRpPerKwh":0}`, "tariffRpPerKwh", "out_of_range", 422},
		{"tariff too high", `{"tariffRpPerKwh":100001}`, "tariffRpPerKwh", "out_of_range", 422},
		{"empty name", `{"businessName":"   "}`, "businessName", "required", 422},
		{"long name", `{"businessName":"` + strings.Repeat("a", 81) + `"}`, "businessName", "too_long", 422},
		{"bad sensitivity", `{"anomalySensitivity":"ekstrem"}`, "anomalySensitivity", "invalid", 422},
		{"unknown zone", `{"timeZone":"Mars/Olympus"}`, "timeZone", "invalid", 422},
		{"UTC is not a zone of a place", `{"timeZone":"UTC"}`, "timeZone", "invalid", 422},
		{"Etc/UTC is not a place", `{"timeZone":"Etc/UTC"}`, "timeZone", "invalid", 422},
		{"GMT is not a place", `{"timeZone":"GMT"}`, "timeZone", "invalid", 422},
		{"posix copy of a zone", `{"timeZone":"posix/Asia/Jakarta"}`, "timeZone", "invalid", 422},
		{"a very long name", `{"timeZone":"` + strings.Repeat("a/", 40) + `"}`, "timeZone", "invalid", 422},
		{"empty body", `{}`, "body", "empty", 422},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.Call(t, http.MethodPatch, "/v1/settings", tt.body, session)
			fields, _ := r.Body["fields"].(map[string]any)

			if r.Status != tt.status || fields[tt.field] != tt.code {
				t.Fatalf("answered %d %v, want %s %s", r.Status, r.Body, tt.field, tt.code)
			}
		})
	}

	for name, body := range map[string]string{
		"unknown key":   `{"colour":"red"}`,
		"string tariff": `{"tariffRpPerKwh":"1500"}`,
		"not an object": `[1]`,
	} {
		if r := e.Call(t, http.MethodPatch, "/v1/settings", body, session); r.Status != http.StatusBadRequest || r.Code() != "invalid_json" {
			t.Errorf("%s answered %d %v", name, r.Status, r.Body)
		}
	}

	// nothing in the failed requests changed the stored settings
	if r := e.Call(t, http.MethodGet, "/v1/settings", "", session); r.Body["businessName"] != "Toko Maju" {
		t.Fatalf("a refused request changed the settings %v", r.Body)
	}
}

func TestRooms(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	if r := e.Call(t, http.MethodGet, "/v1/rooms", "", session); r.Status != http.StatusOK || len(r.Body["items"].([]any)) != 0 {
		t.Fatalf("a new business has rooms %v", r.Body)
	}

	for _, name := range []string{"Ruang produksi", "Gudang", "ruang jahit"} {
		if r := e.Call(t, http.MethodPost, "/v1/rooms", `{"name":"`+name+`"}`, session); r.Status != http.StatusCreated || r.Body["id"] == "" {
			t.Fatalf("create %s answered %d %v", name, r.Status, r.Body)
		}
	}

	if r := e.Call(t, http.MethodPost, "/v1/rooms", `{"name":" GUDANG "}`, session); r.Status != http.StatusConflict || r.Code() != "room_exists" {
		t.Fatalf("a duplicate answered %d %v", r.Status, r.Body)
	}

	if r := e.Call(t, http.MethodPost, "/v1/rooms", `{"name":"`+strings.Repeat("a", 41)+`"}`, session); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a long name answered %d", r.Status)
	}

	r := e.Call(t, http.MethodGet, "/v1/rooms", "", session)

	var names []string
	for _, item := range r.Body["items"].([]any) {
		names = append(names, item.(map[string]any)["name"].(string))
	}

	if strings.Join(names, ",") != "Gudang,ruang jahit,Ruang produksi" {
		t.Fatalf("the rooms are %v, want them by name without regard to case", names)
	}
}

func TestOneBusinessNeverSeesAnother(t *testing.T) {
	e := testutil.New(t)
	budi := e.SignUp(t, "budi@example.com")
	ani := e.SignUp(t, "ani@example.com")

	e.Call(t, http.MethodPost, "/v1/rooms", `{"name":"Gudang Budi"}`, budi)
	e.Call(t, http.MethodPatch, "/v1/settings", `{"tariffRpPerKwh":2000,"businessName":"Toko Budi"}`, budi)

	// the same room name is free in another business
	if r := e.Call(t, http.MethodPost, "/v1/rooms", `{"name":"Gudang Budi"}`, ani); r.Status != http.StatusCreated {
		t.Fatalf("the other business could not use the name %d %v", r.Status, r.Body)
	}

	r := e.Call(t, http.MethodGet, "/v1/settings", "", ani)
	tariff, _ := r.Body["tariff"].(map[string]any)

	if r.Body["businessName"] != "Toko Maju" || tariff["isDefault"] != true {
		t.Fatalf("the settings of another business leaked %v", r.Body)
	}

	if r = e.Call(t, http.MethodGet, "/v1/rooms", "", ani); len(r.Body["items"].([]any)) != 1 {
		t.Fatalf("the rooms of another business leaked %v", r.Body)
	}
}

func TestTooManyRooms(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	e.Exec(t, "INSERT INTO rooms (business_id, name) SELECT id, 'Ruang ' || g FROM businesses, generate_series(1, 100) g")

	if r := e.Call(t, http.MethodPost, "/v1/rooms", `{"name":"Satu lagi"}`, session); r.Status != http.StatusConflict || r.Code() != "too_many_rooms" {
		t.Fatalf("the 101st room answered %d %v", r.Status, r.Body)
	}
}

func TestABusinessWithoutANameShowsAPlaceholder(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	e.Exec(t, "UPDATE businesses SET name = NULL")

	if r := e.Call(t, http.MethodGet, "/v1/settings", "", session); r.Body["businessName"] != "Usaha saya" {
		t.Fatalf("the name is %v", r.Body["businessName"])
	}
}

func TestRoomsAddedAtTheSameTimeStayUnderTheCap(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	e.Exec(t, "INSERT INTO rooms (business_id, name) SELECT id, 'Ruang ' || g FROM businesses, generate_series(1, 95) g")

	var wg sync.WaitGroup

	for i := range 12 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			e.Call(t, http.MethodPost, "/v1/rooms", fmt.Sprintf(`{"name":"Baru %d"}`, i), session)
		}()
	}

	wg.Wait()

	var n int
	if err := e.Pool.QueryRow(t.Context(), "SELECT count(*) FROM rooms").Scan(&n); err != nil || n != 100 {
		t.Fatalf("the business has %d rooms (%v), want 100", n, err)
	}
}

func TestANameThatDrawsNothingIsRefused(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	for _, name := range []string{"\u3164", "\u2800\u2800", "\uffa0", "a\ue000"} {
		body := `{"name":"` + name + `"}`
		if r := e.Call(t, http.MethodPost, "/v1/rooms", body, session); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%q answered %d", name, r.Status)
		}
	}
}
