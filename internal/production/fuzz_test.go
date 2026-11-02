package production

import "testing"

// FuzzParseDay feeds the date check with any text. A date that passes must be written exactly as YYYY-MM-DD, so no other form of
// the same day reaches the database.
func FuzzParseDay(f *testing.F) {
	for _, seed := range []string{"", "2026-11-02", "2026-2-2", "2026-02-30", "2026-11-02T00:00:00Z", " 2026-11-02", "0000-01-01", "9999-12-31", "２０２６-11-02"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		day, err := parseDay(raw)
		if err != nil {
			return
		}

		if day.Format(dateLayout) != raw {
			t.Fatalf("%q passed and reads as %s", raw, day.Format(dateLayout))
		}
	})
}
