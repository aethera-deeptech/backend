package api

import "testing"

func TestKeyOf(t *testing.T) {
	tests := map[string]string{
		"203.0.113.9":                      "203.0.113.9",
		"::ffff:203.0.113.9":               "203.0.113.9",
		"2001:db8:1:2:aaaa:bbbb:cccc:dddd": "2001:db8:1:2::/64",
		"2001:db8:1:2:1111:2222:3333:4444": "2001:db8:1:2::/64",
		"2001:db8:1:3:1111:2222:3333:4444": "2001:db8:1:3::/64",
		"not an address":                   "not an address",
		"":                                 "",
	}

	for in, want := range tests {
		if got := keyOf(in); got != want {
			t.Errorf("keyOf(%q) = %q, want %q", in, got, want)
		}
	}
}
