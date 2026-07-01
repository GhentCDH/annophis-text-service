package server

import (
	"net/http/httptest"
	"net/url"
	"testing"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

func TestNormalizeMode_DefaultAndValidation(t *testing.T) {
	// Absent parameter defaults to nfc.
	if m, ok := normalizeMode(httptest.NewRequest("GET", "/x", nil)); m != "nfc" || !ok {
		t.Fatalf("default: got (%q,%v), want (nfc,true)", m, ok)
	}
	// Accepted values (case-insensitive, trimmed).
	for _, v := range []string{"nfc", "nfd", "nfkc", "nfkd", "strip", "NFD", "  strip  "} {
		r := httptest.NewRequest("GET", "/x?normalize="+url.QueryEscape(v), nil)
		if _, ok := normalizeMode(r); !ok {
			t.Errorf("expected %q to be accepted", v)
		}
	}
	// Unknown value is rejected.
	if _, ok := normalizeMode(httptest.NewRequest("GET", "/x?normalize=bogus", nil)); ok {
		t.Fatalf("expected bogus to be rejected")
	}
}

func TestNormalizeText_Forms(t *testing.T) {
	nfc := norm.NFC.String("Πέρσαι") // precomposed accented Greek

	if got := normalizeText("nfd", nfc); got != norm.NFD.String(nfc) {
		t.Errorf("nfd = %q, want decomposition", got)
	}
	if normalizeText("nfd", nfc) == nfc {
		t.Errorf("expected nfd to differ from nfc for accented Greek")
	}
	if got := normalizeText("nfc", norm.NFD.String(nfc)); got != nfc {
		t.Errorf("nfc did not recompose decomposed input")
	}
}

func TestStripToBase(t *testing.T) {
	cases := map[string]string{
		"Hēródotus": "Herodotus", // Latin macron + acute stripped
		"œuvre":     "oeuvre",    // letter ligature expanded (not done by NFKD)
		"ﬁle":       "file",      // typographic ligature expanded by NFKD
		"straße":    "strasse",   // ß expanded
		"Πέρσαι":    "Περσαι",    // Greek accents removed, base letters kept
	}
	for in, want := range cases {
		if got := stripToBase(in); got != want {
			t.Errorf("stripToBase(%q) = %q, want %q", in, got, want)
		}
	}
	// No combining marks may survive a strip.
	for _, r := range stripToBase("ᾷΠέρσαι") {
		if unicode.Is(unicode.Mn, r) {
			t.Fatalf("combining mark survived strip: %U", r)
		}
	}
}
