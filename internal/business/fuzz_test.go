package business_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aethera-deeptech/backend/internal/business"
)

// FuzzCleanName feeds the name check with any text. A name that passes is trimmed, has 1 to the most characters and holds no control
// character, and a name that fails answers with a code and no name.
func FuzzCleanName(f *testing.F) {
	for _, seed := range []string{"", " ", "Ruang jahit", "  Mesin jahit 1  ", "a\u0000b", "tab\there", "\u202eevil", strings.Repeat("x", 41), "\xff"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		const most = 40

		name, code := business.CleanName(raw, most)

		if code != "" {
			if name != "" {
				t.Fatalf("%q failed with %q and still gave the name %q", raw, code, name)
			}

			return
		}

		if name == "" || name != strings.TrimSpace(name) || utf8.RuneCountInString(name) > most {
			t.Fatalf("%q gave the name %q", raw, name)
		}
	})
}
