package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Budi@Example.COM", "budi@example.com", true},
		{"  budi@example.com ", "budi@example.com", true},
		{"budi+toko@example.co.id", "budi+toko@example.co.id", true},
		{"", "", false},
		{"budi", "", false},
		{"budi@localhost", "", false},
		{"budi@example.", "", false},
		{"Budi <budi@example.com>", "", false},
		{"a@b.c\r\nBcc: x@y.z", "", false},
		{strings.Repeat("a", 250) + "@b.cd", "", false},
	}

	for _, tt := range tests {
		got, ok := normalizeEmail(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("normalizeEmail(%q) = %q, %v, want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestValidPassword(t *testing.T) {
	tests := map[string]string{
		"":                        "required",
		"1234567":                 "too_short",
		"12345678":                "",
		"kata sandi yang panjang": "",
		strings.Repeat("ä", 128):  "",
		strings.Repeat("a", 129):  "too_long",
	}

	for in, want := range tests {
		got, ok := validPassword(in)
		if got != want || ok != (want == "") {
			t.Errorf("validPassword(%d runes) = %q, %v, want %q", len(in), got, ok, want)
		}
	}
}

func TestCleanName(t *testing.T) {
	tests := []struct {
		in, want, code string
		max            int
	}{
		{"  Toko Maju  ", "Toko Maju", "", 80},
		{"", "", "required", 80},
		{"   ", "", "required", 80},
		{strings.Repeat("a", 81), "", "too_long", 80},
		{"Toko\x00Maju", "", "invalid", 80},
		{"Toko\nMaju", "", "invalid", 80},
		{strings.Repeat("é", 80), strings.Repeat("é", 80), "", 80},
	}

	for _, tt := range tests {
		got, code := cleanName(tt.in, tt.max)
		if got != tt.want || code != tt.code {
			t.Errorf("cleanName(%q) = %q, %q, want %q, %q", tt.in, got, code, tt.want, tt.code)
		}
	}
}

func TestHasherRoundTrip(t *testing.T) {
	h := NewHasher(1)
	ctx := context.Background()

	hash, err := h.Hash(ctx, "kata sandi")
	if err != nil {
		t.Fatalf("hash failed %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected form %q", hash)
	}

	if ok, err := h.Verify(ctx, "kata sandi", hash); err != nil || !ok {
		t.Fatalf("the right password failed %v %v", ok, err)
	}

	if ok, _ := h.Verify(ctx, "kata sandi lain", hash); ok {
		t.Fatal("a wrong password matched")
	}

	other, _ := h.Hash(ctx, "kata sandi")
	if other == hash {
		t.Fatal("two hashes of one password are equal, so the salt is not random")
	}
}

func TestHasherRefusesAMalformedOrHugeHash(t *testing.T) {
	h := NewHasher(1)

	for _, encoded := range []string{
		"", "plain", "$argon2id$v=19$m=19456,t=2,p=1$abc",
		"$bcrypt$v=19$m=19456,t=2,p=1$YWJj$YWJj",
		"$argon2id$v=19$m=4000000,t=2,p=1$YWJj$YWJj",
		"$argon2id$v=19$m=19456,t=99,p=1$YWJj$YWJj",
		"$argon2id$v=19$m=19456,t=2,p=0$YWJj$YWJj",
		"$argon2id$v=19$m=19456,t=2,p=1$***$YWJj",
		"$argon2id$v=19$m=19456,t=2,p=1$YWJj$",
	} {
		if ok, err := h.Verify(context.Background(), "x", encoded); ok || err != nil {
			t.Errorf("Verify(%q) = %v, %v, want false and no error", encoded, ok, err)
		}
	}
}

func TestHasherIsBusyWhenEverySlotIsTaken(t *testing.T) {
	h := NewHasher(1)
	h.slots <- struct{}{}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := h.Hash(ctx, "x"); err != ErrBusy { //nolint:errorlint // the value itself
		t.Fatalf("error is %v, want ErrBusy", err)
	}
}

func TestTokens(t *testing.T) {
	a, hashA, err := newToken()
	if err != nil {
		t.Fatal(err)
	}

	b, _, _ := newToken()

	if a == b || len(a) != 43 || len(hashA) != 32 {
		t.Fatalf("tokens %q and %q, hash length %d", a, b, len(hashA))
	}

	if string(hashToken(a)) != string(hashA) {
		t.Fatal("the hash of a token changes")
	}
}
