package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const txtStem = "urn:cts:evwrit:p_oxy_1234.txtparsed:"

// newTxtRouter builds a router backed only by a txt_data directory (no CEX).
// The fixture has an empty second line, which must be skipped while numbering
// stays consecutive.
func newTxtRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	ns := filepath.Join(dir, "evwrit")
	if err := os.MkdirAll(ns, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "alpha beta\n\ngamma delta\nepsilon\n"
	if err := os.WriteFile(filepath.Join(ns, "p_oxy_1234.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return BuildRouter(NewServer(ServerConfig{TxtData: dir})), dir
}

func TestTxt_LineNodesConsecutive(t *testing.T) {
	h, _ := newTxtRouter(t)

	if nr := getNodes(t, h, "/texts/"+txtStem+"1"); nr.Status != "Success" || nr.Nodes[0].Text[0] != "alpha beta" {
		t.Fatalf("line 1: %+v", nr.Nodes)
	}
	// The empty line is skipped, so "gamma delta" is line 2 (not 3).
	if nr := getNodes(t, h, "/texts/"+txtStem+"2"); nr.Status != "Success" || !strings.Contains(nr.Nodes[0].Text[0], "gamma") {
		t.Fatalf("line 2 should be gamma: %+v", nr.Nodes)
	}
	if nr := getNodes(t, h, "/texts/"+txtStem+"3"); nr.Status != "Success" || nr.Nodes[0].Text[0] != "epsilon" {
		t.Fatalf("line 3 should be epsilon: %+v", nr.Nodes)
	}
	if nr := getNodes(t, h, "/texts/"+txtStem+"4"); nr.Status != "Exception" {
		t.Fatalf("expected no line 4, got %s", nr.Status)
	}
}

func TestTxt_WorkListAndCatalog(t *testing.T) {
	h, _ := newTxtRouter(t)

	ur := getURNs(t, h, "/texts")
	found := false
	for _, u := range ur.URN {
		if u == txtStem {
			found = true
		}
	}
	if !found {
		t.Fatalf("work stem missing from /texts: %v", ur.URN)
	}

	req := httptest.NewRequest(http.MethodGet, "/texts/catalog", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var cr CatalogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &cr); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	var e *CatalogEntry
	for i := range cr.Entries {
		if cr.Entries[i].URN == txtStem {
			e = &cr.Entries[i]
		}
	}
	if e == nil || e.GroupName != "evwrit" || e.WorkTitle != "p_oxy_1234" || e.CitationScheme != "line" {
		t.Fatalf("synthetic catalog entry wrong: %+v", cr.Entries)
	}
}

func TestTxt_RangeAndNav(t *testing.T) {
	h, _ := newTxtRouter(t)

	if nr := getNodes(t, h, "/texts/"+txtStem+"1-3"); nr.Status != "Success" || len(nr.Nodes) != 3 {
		t.Fatalf("range: status=%s n=%d", nr.Status, len(nr.Nodes))
	}
	if first := getNodes(t, h, "/texts/first/"+txtStem+"2"); first.Nodes[0].URN[0] != txtStem+"1" {
		t.Fatalf("first=%v", first.Nodes[0].URN)
	}
	if next := getNodes(t, h, "/texts/next/"+txtStem+"1"); next.Nodes[0].URN[0] != txtStem+"2" {
		t.Fatalf("next=%v", next.Nodes[0].URN)
	}
	if last := getNodes(t, h, "/texts/last/"+txtStem+"1"); last.Nodes[0].URN[0] != txtStem+"3" {
		t.Fatalf("last=%v", last.Nodes[0].URN)
	}
}

func TestTxt_AnchoredAndHash(t *testing.T) {
	h, _ := newTxtRouter(t)

	nr := getNodes(t, h, "/texts/"+txtStem+"1@beta[1]?clip=false")
	if nr.Status != "Success" || !strings.Contains(nr.Nodes[0].Text[0], "beta") {
		t.Fatalf("anchored lookup on line: %+v", nr.Nodes)
	}

	hr := getHash(t, h, "/texts/hash/"+txtStem+"1")
	if hr.Status != "Success" || len(hr.Hashes) != 1 {
		t.Fatalf("hash: %+v", hr)
	}
	if hr.Hashes[0].Hash != sha1hex("alpha beta") {
		t.Fatalf("txt line hash mismatch")
	}
}

func TestTxt_RescanOnChange(t *testing.T) {
	h, dir := newTxtRouter(t)
	f := filepath.Join(dir, "evwrit", "p_oxy_1234.txt")

	if nr := getNodes(t, h, "/texts/"+txtStem+"1"); nr.Nodes[0].Text[0] != "alpha beta" {
		t.Fatalf("initial content wrong")
	}

	time.Sleep(10 * time.Millisecond) // let mtime advance
	if err := os.WriteFile(f, []byte("CHANGED first\nsecond\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if nr := getNodes(t, h, "/texts/"+txtStem+"1"); nr.Nodes[0].Text[0] != "CHANGED first" {
		t.Fatalf("rescan not applied, got %q", nr.Nodes[0].Text[0])
	}
}

func TestTxt_CEXUnaffectedWhenTxtUnset(t *testing.T) {
	// Sanity: with txt_data unset, loadCorpus is exactly the CEX path.
	h := newTestRouter(t)
	if nr := getNodes(t, h, "/texts/urn:cts:greekLit:tlg0016.tlg001.grc:1.1"); nr.Status != "Success" {
		t.Fatalf("CEX path regressed: %s", nr.Status)
	}
}
