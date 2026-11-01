package production_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

func jakarta(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}

	return loc
}

func day(t *testing.T, offset int) string {
	t.Helper()

	return time.Now().In(jakarta(t)).AddDate(0, 0, offset).Format("2006-01-02")
}

func TestProductionNeedsASession(t *testing.T) {
	e := testutil.New(t)

	for _, route := range [][2]string{{http.MethodGet, "/v1/production"}, {http.MethodGet, "/v1/production/2026-10-01"}, {http.MethodPut, "/v1/production/2026-10-01"}} {
		if r := e.Call(t, route[0], route[1], `{"quantity":1,"unit":"potong"}`, ""); r.Status != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d without a session", route[0], route[1], r.Status)
		}
	}
}

func TestAnEmptyBusinessHasNoRecords(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	r := e.Call(t, http.MethodGet, "/v1/production", "", session)
	units, _ := r.Body["units"].([]any)
	recent, _ := r.Body["recent"].([]any)

	if r.Status != http.StatusOK || r.Body["unit"] != "potong" || len(units) < 2 || units[0] != "potong" || len(recent) != 0 || r.Body["today"] != day(t, 0) {
		t.Fatalf("answered %d %v", r.Status, r.Body)
	}

	if r = e.Call(t, http.MethodGet, "/v1/production/"+day(t, -1), "", session); r.Status != http.StatusNotFound {
		t.Fatalf("a day without a record answered %d", r.Status)
	}
}

func TestSaveReplaceAndRead(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")
	yesterday := day(t, -1)

	r := e.Call(t, http.MethodPut, "/v1/production/"+yesterday, `{"quantity":124,"unit":"potong"}`, session)
	if r.Status != http.StatusOK || r.Body["date"] != yesterday || r.Body["quantity"] != float64(124) {
		t.Fatalf("save answered %d %v", r.Status, r.Body)
	}

	// a second save of the same day replaces the first one
	if r = e.Call(t, http.MethodPut, "/v1/production/"+yesterday, `{"quantity":130,"unit":"potong"}`, session); r.Status != http.StatusOK {
		t.Fatalf("replace answered %d %v", r.Status, r.Body)
	}

	if r = e.Call(t, http.MethodGet, "/v1/production/"+yesterday, "", session); r.Body["quantity"] != float64(130) {
		t.Fatalf("read answered %v", r.Body)
	}

	// today is allowed, and the list is newest first and holds the 3 newest
	for i := range 3 {
		e.Call(t, http.MethodPut, "/v1/production/"+day(t, -2-i), `{"quantity":10,"unit":"potong"}`, session)
	}

	e.Call(t, http.MethodPut, "/v1/production/"+day(t, 0), `{"quantity":55,"unit":"potong"}`, session)

	r = e.Call(t, http.MethodGet, "/v1/production", "", session)
	recent, _ := r.Body["recent"].([]any)

	if len(recent) != 3 || recent[0].(map[string]any)["date"] != day(t, 0) || recent[1].(map[string]any)["date"] != yesterday {
		t.Fatalf("recent is %v", recent)
	}
}

func TestSaveValidation(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	tests := []struct {
		name, path, body, field, code string
		status                        int
	}{
		{"a future day", day(t, 1), `{"quantity":5,"unit":"potong"}`, "date", "in_the_future", 422},
		{"zero", day(t, -1), `{"quantity":0,"unit":"potong"}`, "quantity", "out_of_range", 422},
		{"negative", day(t, -1), `{"quantity":-3,"unit":"potong"}`, "quantity", "out_of_range", 422},
		{"too many", day(t, -1), `{"quantity":100000001,"unit":"potong"}`, "quantity", "out_of_range", 422},
		{"a day that is too old", day(t, -401), `{"quantity":5,"unit":"potong"}`, "date", "too_old", 422},
		{"no quantity", day(t, -1), `{"unit":"potong"}`, "quantity", "required", 422},
		{"a null quantity", day(t, -1), `{"quantity":null,"unit":"potong"}`, "quantity", "required", 422},
		{"no unit", day(t, -1), `{"quantity":5}`, "unit", "required", 422},
		{"unknown unit", day(t, -1), `{"quantity":5,"unit":"ton"}`, "unit", "invalid", 422},
		{"not a date", "kemarin", `{"quantity":5,"unit":"potong"}`, "date", "invalid", 422},
		{"impossible date", "2026-02-30", `{"quantity":5,"unit":"potong"}`, "date", "invalid", 422},
		{"date with a time", "2026-10-01T10:00", `{"quantity":5,"unit":"potong"}`, "date", "invalid", 422},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.Call(t, http.MethodPut, "/v1/production/"+tt.path, tt.body, session)
			fields, _ := r.Body["fields"].(map[string]any)

			if r.Status != tt.status || fields[tt.field] != tt.code {
				t.Fatalf("answered %d %v, want %s %s", r.Status, r.Body, tt.field, tt.code)
			}
		})
	}

	for name, body := range map[string]string{"a decimal": `{"quantity":1.5,"unit":"potong"}`, "a string": `{"quantity":"5","unit":"potong"}`, "an extra key": `{"quantity":5,"unit":"potong","x":1}`} {
		if r := e.Call(t, http.MethodPut, "/v1/production/"+day(t, -1), body, session); r.Status != http.StatusBadRequest {
			t.Errorf("%s answered %d", name, r.Status)
		}
	}
}

func TestTheUnitOfTheLastSaveIsRemembered(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	e.Call(t, http.MethodPut, "/v1/production/"+day(t, -1), `{"quantity":40,"unit":"kg"}`, session)

	r := e.Call(t, http.MethodGet, "/v1/production", "", session)
	if r.Body["unit"] != "kg" {
		t.Fatalf("the unit is %v, want kg", r.Body["unit"])
	}

	// the record keeps the unit it was counted in
	e.Call(t, http.MethodPut, "/v1/production/"+day(t, -2), `{"quantity":9,"unit":"potong"}`, session)

	if r = e.Call(t, http.MethodGet, "/v1/production/"+day(t, -1), "", session); r.Body["unit"] != "kg" {
		t.Fatalf("an older record changed its unit %v", r.Body)
	}
}

func TestOneBusinessNeverSeesTheRecordsOfAnother(t *testing.T) {
	e := testutil.New(t)
	budi := e.SignUp(t, "budi@example.com")
	ani := e.SignUp(t, "ani@example.com")

	e.Call(t, http.MethodPut, "/v1/production/"+day(t, -1), `{"quantity":77,"unit":"potong"}`, budi)

	if r := e.Call(t, http.MethodGet, "/v1/production/"+day(t, -1), "", ani); r.Status != http.StatusNotFound {
		t.Fatalf("the record of another business answered %d %v", r.Status, r.Body)
	}

	if r := e.Call(t, http.MethodGet, "/v1/production", "", ani); len(r.Body["recent"].([]any)) != 0 {
		t.Fatalf("the list of another business leaked %v", r.Body)
	}
}

func TestTheDayFollowsTheTimeZoneOfTheBusiness(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	// Kiritimati is 14 hours ahead of UTC, so its day is never behind the day of Jakarta and is often a day ahead
	e.Exec(t, "UPDATE businesses SET time_zone = 'Pacific/Kiritimati'")

	loc, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatal(err)
	}

	want := time.Now().In(loc).Format("2006-01-02")

	if r := e.Call(t, http.MethodGet, "/v1/production", "", session); r.Body["today"] != want {
		t.Fatalf("today is %v, want %s", r.Body["today"], want)
	}
}
