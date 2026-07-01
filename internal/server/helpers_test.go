package server

import (
	"net/http/httptest"
	"testing"
)

// Greek fixtures. The accent-sensitive ones use explicit code points so the
// NFC/NFD state is unambiguous regardless of how the editor stores this file.
const (
	persaiNFC      = "Πέρσαι"   // Πέρσαι, precomposed έ
	persaiPlain    = "Περσαι"   // unaccented
	anthroposPlain = "ανθρωπος" // unaccented
	etonosNFC      = "έ"        // precomposed έ
	etonosNFD      = "έ"       // decomposed ε + combining acute
)

func TestFindNthInsensitive_GreekCaseAndSigma(t *testing.T) {
	// Case-insensitive and sigma-variant (ς/σ/Σ) folding, accent-sensitive.
	// "λόγος" ends in final sigma ς; the needle uses medial σ and mixed case.
	logos := "λόγος"  // final ς
	needle := "Λόγοσ" // cap lambda, medial σ
	if start, end := findNthInsensitive(logos, needle, 1); start != 0 || end != 5 {
		t.Fatalf("got (%d,%d), want (0,5)", start, end)
	}

	// Accents still matter without folding: unaccented needle must not match.
	if s, _ := findNthInsensitive(persaiNFC, persaiPlain, 1); s != -1 {
		t.Fatalf("expected no match for unaccented needle, got start=%d", s)
	}
}

func TestFindNthFolded_IgnoresDiacritics(t *testing.T) {
	// Unaccented needle should match precomposed έ, and offsets must map back
	// onto the original (accented) runes.
	start, end := findNthFolded(persaiNFC, persaiPlain, 1)
	if start != 0 || end != 6 {
		t.Fatalf("got (%d,%d), want (0,6)", start, end)
	}
	if got := string([]rune(persaiNFC)[start:end]); got != persaiNFC {
		t.Fatalf("sliced %q, want original accented form", got)
	}
}

func TestFindNthFolded_NthOccurrence(t *testing.T) {
	// Accented then unaccented spelling of the same word; folding unifies them.
	hay := "ἄνθρωπος " + anthroposPlain
	start, _ := findNthFolded(hay, anthroposPlain, 2)
	rns := []rune(hay)
	n := len([]rune(anthroposPlain))
	if start < 0 || start+n > len(rns) || string(rns[start:start+n]) != anthroposPlain {
		t.Fatalf("2nd folded occurrence not located correctly, start=%d", start)
	}
}

func TestFindAnchorMatch_HonorsIgnoreAccentsFlag(t *testing.T) {
	rPlain := httptest.NewRequest("GET", "/texts/x", nil)
	if s, _ := findAnchorMatch(rPlain, persaiNFC, persaiPlain, 1); s != -1 {
		t.Fatalf("without flag expected no accent-insensitive match, got %d", s)
	}
	rFold := httptest.NewRequest("GET", "/texts/x?ignoreAccents=true", nil)
	if s, _ := findAnchorMatch(rFold, persaiNFC, persaiPlain, 1); s != 0 {
		t.Fatalf("with flag expected match at 0, got %d", s)
	}
}

func TestParseRefAnchorToken_NormalisesNeedleToNFC(t *testing.T) {
	// Feed an NFD-encoded needle; parseRefAnchorToken must return NFC.
	tok := "1.0@" + etonosNFD + "[2]"
	ref, needle, occ, anchored := parseRefAnchorToken(tok)
	if !anchored || ref != "1.0" || occ != 2 {
		t.Fatalf("parse got ref=%q occ=%d anchored=%v", ref, occ, anchored)
	}
	if needle != etonosNFC {
		t.Fatalf("needle = %q (% x), want NFC %q (% x)", needle, []rune(needle), etonosNFC, []rune(etonosNFC))
	}
}

func TestParseAnchoredURN(t *testing.T) {
	base, needle, occ, ok := parseAnchoredURN("urn:cts:greekLit:tlg0016.tlg001.grc:1.1@Πέρσαι[2]")
	if !ok || base != "urn:cts:greekLit:tlg0016.tlg001.grc:1.1" || needle != "Πέρσαι" || occ != 2 {
		t.Fatalf("got base=%q needle=%q occ=%d ok=%v", base, needle, occ, ok)
	}
	// No [n] suffix defaults to occurrence 1.
	if _, _, occ, ok := parseAnchoredURN("urn:cts:greekLit:tlg0016.tlg001.grc:1.1@λόγος"); !ok || occ != 1 {
		t.Fatalf("default occ: occ=%d ok=%v", occ, ok)
	}
	// Missing needle is malformed.
	if _, _, _, ok := parseAnchoredURN("urn:cts:greekLit:tlg0016.tlg001.grc:1.1@"); ok {
		t.Fatalf("expected malformed for empty needle")
	}
	// NFD needle in the URN normalises to NFC.
	if _, needle, _, ok := parseAnchoredURN("urn:x:y:z.z.z:1@" + etonosNFD); !ok || needle != etonosNFC {
		t.Fatalf("NFC needle: needle=%q ok=%v", needle, ok)
	}
}

func TestClipToSubstring(t *testing.T) {
	full := "abcNEEDLExyz"
	// Case-insensitive match with 2 runes of context on each side.
	out, complete := clipToSubstring(full, "needle", 2)
	if out != "bcNEEDLExy" || complete {
		t.Fatalf("out=%q complete=%v", out, complete)
	}
	// Missing needle returns the full string, complete.
	if out, complete := clipToSubstring(full, "zzz", 2); out != full || !complete {
		t.Fatalf("miss: out=%q complete=%v", out, complete)
	}
}

func TestSliceHelpers(t *testing.T) {
	rns := []rune("Πέρσαι")
	if out, complete := sliceFromRunes(rns, 2); out != "ρσαι" || complete {
		t.Fatalf("from: out=%q complete=%v", out, complete)
	}
	if out, complete := sliceUntilRunes(rns, 2); out != "Πέ" || complete {
		t.Fatalf("until: out=%q complete=%v", out, complete)
	}
	if out, complete := sliceBetweenRunes(rns, 1, 3); out != "έρ" || complete {
		t.Fatalf("between: out=%q complete=%v", out, complete)
	}
	// Full-span slice is complete.
	if out, complete := sliceBetweenRunes(rns, 0, len(rns)); out != "Πέρσαι" || !complete {
		t.Fatalf("full: out=%q complete=%v", out, complete)
	}
}

func TestWorkStemAndSameWork(t *testing.T) {
	a := "urn:cts:greekLit:tlg0016.tlg001.grc:1.1"
	b := "urn:cts:greekLit:tlg0016.tlg001.grc:2.5"
	c := "urn:cts:latinLit:phi1038.phi001.lat:1.1"
	if got := workStem(a); got != "urn:cts:greekLit:tlg0016.tlg001.grc:" {
		t.Fatalf("stem=%q", got)
	}
	if !sameWork(a, b) {
		t.Fatalf("expected same work for a,b")
	}
	if sameWork(a, c) {
		t.Fatalf("expected different work for a,c")
	}
}
