package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// A tiny CEX fixture: one Greek work with three passages (1.1 repeats "Πέρσαι"
// so occurrence indexing can be tested) plus one Latin passage in a second work
// so work-boundary behaviour (stems, next/previous) can be checked.
const testCEX = `#!cexversion
3.0

#!ctscatalog
urn#citationScheme#groupName#workTitle#versionLabel#exemplarLabel#online
urn:cts:greekLit:tlg0016.tlg001.grc:#book#Herodotus#Histories#Greek##true

#!ctsdata
urn:cts:greekLit:tlg0016.tlg001.grc:1.0#πρῶτον μὲν λόγος
urn:cts:greekLit:tlg0016.tlg001.grc:1.1#Πέρσαι μὲν καὶ Πέρσαι πάλιν
urn:cts:greekLit:tlg0016.tlg001.grc:1.2#τέλος τοῦ λόγου
urn:cts:latinLit:phi1038.phi001.lat:1.1#Gallia est omnis
`

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(testCEX))
	}))
	t.Cleanup(ts.Close)
	return BuildRouter(NewServer(ServerConfig{TestSource: ts.URL}))
}

func getNodes(t *testing.T, h http.Handler, target string) NodeResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var nr NodeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &nr); err != nil {
		t.Fatalf("decode %s: %v (body=%s)", target, err, rec.Body.String())
	}
	return nr
}

func getURNs(t *testing.T, h http.Handler, target string) URNResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var ur URNResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ur); err != nil {
		t.Fatalf("decode %s: %v (body=%s)", target, err, rec.Body.String())
	}
	return ur
}

func TestExactNode(t *testing.T) {
	h := newTestRouter(t)
	nr := getNodes(t, h, "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.1")
	if nr.Status != "Success" || len(nr.Nodes) != 1 {
		t.Fatalf("status=%s nodes=%d", nr.Status, len(nr.Nodes))
	}
	n := nr.Nodes[0]
	if n.URN[0] != "urn:cts:greekLit:tlg0016.tlg001.grc:1.1" {
		t.Fatalf("urn=%v", n.URN)
	}
	if !strings.Contains(n.Text[0], "Πέρσαι") || !n.Complete {
		t.Fatalf("text=%q complete=%v", n.Text[0], n.Complete)
	}
	// neighbours within the same work
	if len(n.Previous) != 1 || n.Previous[0] != "urn:cts:greekLit:tlg0016.tlg001.grc:1.0" {
		t.Fatalf("previous=%v", n.Previous)
	}
	if len(n.Next) != 1 || n.Next[0] != "urn:cts:greekLit:tlg0016.tlg001.grc:1.2" {
		t.Fatalf("next=%v", n.Next)
	}
}

func TestPrefixExpansion(t *testing.T) {
	h := newTestRouter(t)
	nr := getNodes(t, h, "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1")
	if nr.Status != "Success" || len(nr.Nodes) != 3 {
		t.Fatalf("status=%s nodes=%d", nr.Status, len(nr.Nodes))
	}
}

func TestAnchoredSingle_OccurrenceAndContext(t *testing.T) {
	h := newTestRouter(t)
	// 2nd "Πέρσαι" with a little context; default clip=true for anchors.
	nr := getNodes(t, h, "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.1@Πέρσαι[2]?context=3")
	if nr.Status != "Success" || len(nr.Nodes) != 1 {
		t.Fatalf("status=%s nodes=%d msg=%q", nr.Status, len(nr.Nodes), nr.Message)
	}
	txt := nr.Nodes[0].Text[0]
	if !strings.Contains(txt, "Πέρσαι") {
		t.Fatalf("snippet %q missing needle", txt)
	}
	// A clipped snippet must report complete=false.
	if nr.Nodes[0].Complete {
		t.Fatalf("expected complete=false for clipped anchor snippet")
	}
}

func TestAnchoredSingle_NotFound(t *testing.T) {
	h := newTestRouter(t)
	nr := getNodes(t, h, "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.1@Πέρσαι[3]")
	if nr.Status != "Exception" {
		t.Fatalf("expected Exception for missing occurrence, got %s", nr.Status)
	}
}

func TestAnchoredIgnoreAccents(t *testing.T) {
	h := newTestRouter(t)
	base := "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.1@Περσαι[1]"
	// Without the flag the unaccented needle should not match the accented text.
	if nr := getNodes(t, h, base); nr.Status != "Exception" {
		t.Fatalf("expected Exception without ignoreAccents, got %s", nr.Status)
	}
	// With the flag it matches, and the returned snippet keeps its accents.
	nr := getNodes(t, h, base+"?ignoreAccents=true&clip=false")
	if nr.Status != "Success" || len(nr.Nodes) != 1 {
		t.Fatalf("status=%s nodes=%d msg=%q", nr.Status, len(nr.Nodes), nr.Message)
	}
	if !strings.Contains(nr.Nodes[0].Text[0], "Πέρσαι") {
		t.Fatalf("expected accented text back, got %q", nr.Nodes[0].Text[0])
	}
}

func TestAnchoredRegex(t *testing.T) {
	h := newTestRouter(t)
	// The regex delimiters "/" must be percent-encoded so the router does not
	// treat them as path separators.
	nr := getNodes(t, h, "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.0@%2Fλ.γος%2F[1]?clip=false")
	if nr.Status != "Success" || len(nr.Nodes) != 1 {
		t.Fatalf("status=%s nodes=%d msg=%q", nr.Status, len(nr.Nodes), nr.Message)
	}
}

func TestRangeAcrossNodes(t *testing.T) {
	h := newTestRouter(t)
	nr := getNodes(t, h, "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.0-1.2")
	if nr.Status != "Success" || len(nr.Nodes) != 3 {
		t.Fatalf("status=%s nodes=%d", nr.Status, len(nr.Nodes))
	}
	if nr.Nodes[0].URN[0] != "urn:cts:greekLit:tlg0016.tlg001.grc:1.0" ||
		nr.Nodes[2].URN[0] != "urn:cts:greekLit:tlg0016.tlg001.grc:1.2" {
		t.Fatalf("range bounds wrong: %v .. %v", nr.Nodes[0].URN, nr.Nodes[2].URN)
	}
}

func TestRangeWithAnchors_WithinOneNode(t *testing.T) {
	h := newTestRouter(t)
	// Start at first "Πέρσαι", end at second — both inside 1.1.
	nr := getNodes(t, h, "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.1@Πέρσαι[1]-@Πέρσαι[2]")
	if nr.Status != "Success" || len(nr.Nodes) != 1 {
		t.Fatalf("status=%s nodes=%d msg=%q", nr.Status, len(nr.Nodes), nr.Message)
	}
	txt := nr.Nodes[0].Text[0]
	if !strings.HasPrefix(txt, "Πέρσαι") || !strings.HasSuffix(txt, "Πέρσαι") {
		t.Fatalf("between-anchors slice wrong: %q", txt)
	}
}

func TestNavigation(t *testing.T) {
	h := newTestRouter(t)
	first := getNodes(t, h, "/texts/first/urn:cts:greekLit:tlg0016.tlg001.grc:1.1")
	if first.Nodes[0].URN[0] != "urn:cts:greekLit:tlg0016.tlg001.grc:1.0" {
		t.Fatalf("first=%v", first.Nodes[0].URN)
	}
	last := getNodes(t, h, "/texts/last/urn:cts:greekLit:tlg0016.tlg001.grc:1.1")
	if last.Nodes[0].URN[0] != "urn:cts:greekLit:tlg0016.tlg001.grc:1.2" {
		t.Fatalf("last=%v", last.Nodes[0].URN)
	}
	// next must not cross into the Latin work.
	next := getNodes(t, h, "/texts/next/urn:cts:greekLit:tlg0016.tlg001.grc:1.2")
	if next.Status != "Exception" {
		t.Fatalf("expected no next past work end, got %s -> %v", next.Status, next.Nodes)
	}
}

func TestWorkStems(t *testing.T) {
	h := newTestRouter(t)
	ur := getURNs(t, h, "/texts")
	if ur.Status != "Success" || len(ur.URN) != 2 {
		t.Fatalf("status=%s stems=%v", ur.Status, ur.URN)
	}
}

func TestURNExpansionRange(t *testing.T) {
	h := newTestRouter(t)
	ur := getURNs(t, h, "/texts/urns/urn:cts:greekLit:tlg0016.tlg001.grc:1.0-1.2")
	if ur.Status != "Success" || len(ur.URN) != 3 {
		t.Fatalf("status=%s urns=%v", ur.Status, ur.URN)
	}
}

// A malformed row (extra '#' → 3 fields) must be skipped without truncating the
// rows that follow it.
const testCEXMalformed = `#!ctsdata
urn:cts:greekLit:tlg0016.tlg001.grc:1.0#alpha
urn:cts:greekLit:tlg0016.tlg001.grc:1.1#bad#row#here
urn:cts:greekLit:tlg0016.tlg001.grc:1.2#gamma
`

func TestParse_SkipsMalformedRow(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(testCEXMalformed))
	}))
	t.Cleanup(ts.Close)
	h := BuildRouter(NewServer(ServerConfig{TestSource: ts.URL}))

	// The row after the malformed one must still be reachable.
	nr := getNodes(t, h, "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.2")
	if nr.Status != "Success" || len(nr.Nodes) != 1 || nr.Nodes[0].Text[0] != "gamma" {
		t.Fatalf("status=%s nodes=%d text=%q", nr.Status, len(nr.Nodes), func() string {
			if len(nr.Nodes) > 0 {
				return nr.Nodes[0].Text[0]
			}
			return ""
		}())
	}
}

func TestNormalizeParam(t *testing.T) {
	h := newTestRouter(t)
	urn := "urn:cts:greekLit:tlg0016.tlg001.grc:1.1" // "Πέρσαι μὲν καὶ Πέρσαι πάλιν"

	// Default (no param) equals explicit nfc.
	def := getNodes(t, h, "/texts/"+urn)
	nfc := getNodes(t, h, "/texts/"+urn+"?normalize=nfc")
	if def.Status != "Success" || def.Nodes[0].Text[0] != nfc.Nodes[0].Text[0] {
		t.Fatalf("default should equal nfc")
	}

	// nfd is the decomposition of nfc and differs from it.
	nfd := getNodes(t, h, "/texts/"+urn+"?normalize=nfd")
	if nfd.Nodes[0].Text[0] != norm.NFD.String(nfc.Nodes[0].Text[0]) {
		t.Fatalf("nfd is not the decomposition of nfc")
	}
	if nfd.Nodes[0].Text[0] == nfc.Nodes[0].Text[0] {
		t.Fatalf("expected nfd to differ from nfc for accented Greek")
	}

	// strip removes all combining marks.
	strip := getNodes(t, h, "/texts/"+urn+"?normalize=strip")
	for _, r := range strip.Nodes[0].Text[0] {
		if unicode.Is(unicode.Mn, r) {
			t.Fatalf("combining mark in strip output: %q", strip.Nodes[0].Text[0])
		}
	}

	// Unknown value is rejected.
	if bad := getNodes(t, h, "/texts/"+urn+"?normalize=bogus"); bad.Status != "Exception" {
		t.Fatalf("expected Exception for unsupported normalize value, got %s", bad.Status)
	}
}

func TestCatalog(t *testing.T) {
	h := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/texts/catalog", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var cr CatalogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &cr); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	if cr.Status != "Success" || len(cr.Entries) != 1 {
		t.Fatalf("status=%s entries=%d", cr.Status, len(cr.Entries))
	}
	if cr.Entries[0].GroupName != "Herodotus" || cr.Entries[0].WorkTitle != "Histories" {
		t.Fatalf("catalog entry wrong: %+v", cr.Entries[0])
	}
}
