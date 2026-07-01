package server

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getHash(t *testing.T, h http.Handler, target string) HashResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var hr HashResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &hr); err != nil {
		t.Fatalf("decode %s: %v (body=%s)", target, err, rec.Body.String())
	}
	return hr
}

func sha1hex(s string) string   { sum := sha1.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
func sha256hex(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }

func TestHashExact_MatchesTextEndpoint(t *testing.T) {
	h := newTestRouter(t)
	urn := "urn:cts:greekLit:tlg0016.tlg001.grc:1.1"

	hr := getHash(t, h, "/texts/hash/"+urn)
	if hr.Status != "Success" || hr.Algorithm != "sha1" || len(hr.Hashes) != 1 {
		t.Fatalf("status=%s algo=%s n=%d", hr.Status, hr.Algorithm, len(hr.Hashes))
	}
	// The hash must be sha1 of exactly the text /texts/{URN} returns (nfc default).
	nr := getNodes(t, h, "/texts/"+urn)
	if want := sha1hex(nr.Nodes[0].Text[0]); hr.Hashes[0].Hash != want {
		t.Fatalf("hash=%s want=%s", hr.Hashes[0].Hash, want)
	}
	if hr.Hashes[0].URN != urn {
		t.Fatalf("urn=%s", hr.Hashes[0].URN)
	}
}

func TestHashRange_PerNode(t *testing.T) {
	h := newTestRouter(t)
	hr := getHash(t, h, "/texts/hash/urn:cts:greekLit:tlg0016.tlg001.grc:1.0-1.2")
	if hr.Status != "Success" || len(hr.Hashes) != 3 {
		t.Fatalf("status=%s n=%d", hr.Status, len(hr.Hashes))
	}
	// per-node URNs are populated
	for _, nh := range hr.Hashes {
		if nh.URN == "" || len(nh.Hash) != 40 {
			t.Fatalf("bad node hash entry: %+v", nh)
		}
	}
}

func TestHash_Prefix(t *testing.T) {
	h := newTestRouter(t)
	prefix := "urn:cts:greekLit:tlg0016.tlg001.grc:1" // matches 1.0, 1.1, 1.2
	hr := getHash(t, h, "/texts/hash/"+prefix)
	nr := getNodes(t, h, "/texts/"+prefix)
	if hr.Status != "Success" || len(hr.Hashes) < 2 || len(hr.Hashes) != len(nr.Nodes) {
		t.Fatalf("hashes=%d nodes=%d", len(hr.Hashes), len(nr.Nodes))
	}
	for i, nh := range hr.Hashes {
		if nh.URN != nr.Nodes[i].URN[0] || nh.Hash != sha1hex(nr.Nodes[i].Text[0]) {
			t.Fatalf("prefix node %d mismatch", i)
		}
	}
}

func TestHash_Anchored(t *testing.T) {
	h := newTestRouter(t)
	// Anchored + clipped: the hash must be over the clipped result (default clip).
	anchor := "urn:cts:greekLit:tlg0016.tlg001.grc:1.1@Πέρσαι[2]"
	hr := getHash(t, h, "/texts/hash/"+anchor)
	nr := getNodes(t, h, "/texts/"+anchor)
	if hr.Status != "Success" || len(hr.Hashes) != 1 || len(nr.Nodes) != 1 {
		t.Fatalf("status=%s hashes=%d nodes=%d", hr.Status, len(hr.Hashes), len(nr.Nodes))
	}
	if hr.Hashes[0].Hash != sha1hex(nr.Nodes[0].Text[0]) {
		t.Fatalf("anchored hash does not match clipped text")
	}
}

func TestHash_SHA256(t *testing.T) {
	h := newTestRouter(t)
	urn := "urn:cts:greekLit:tlg0016.tlg001.grc:1.1"
	hr := getHash(t, h, "/texts/hash/"+urn+"?algorithm=sha256")
	if hr.Algorithm != "sha256" || len(hr.Hashes[0].Hash) != 64 {
		t.Fatalf("algo=%s len=%d", hr.Algorithm, len(hr.Hashes[0].Hash))
	}
	nr := getNodes(t, h, "/texts/"+urn)
	if hr.Hashes[0].Hash != sha256hex(nr.Nodes[0].Text[0]) {
		t.Fatalf("sha256 mismatch")
	}
}

func TestHash_InvalidAlgorithm(t *testing.T) {
	h := newTestRouter(t)
	hr := getHash(t, h, "/texts/hash/urn:cts:greekLit:tlg0016.tlg001.grc:1.1?algorithm=md5")
	if hr.Status != "Exception" {
		t.Fatalf("expected Exception for unsupported algorithm, got %s", hr.Status)
	}
}

func TestHash_NormalizeAffectsDigest(t *testing.T) {
	h := newTestRouter(t)
	urn := "urn:cts:greekLit:tlg0016.tlg001.grc:1.1"
	nfc := getHash(t, h, "/texts/hash/"+urn) // default nfc
	nfd := getHash(t, h, "/texts/hash/"+urn+"?normalize=nfd")
	if nfc.Hashes[0].Hash == nfd.Hashes[0].Hash {
		t.Fatalf("expected nfc and nfd hashes to differ for accented text")
	}
	// nfd hash must match sha1 of the nfd text the text endpoint returns.
	nr := getNodes(t, h, "/texts/"+urn+"?normalize=nfd")
	if nfd.Hashes[0].Hash != sha1hex(nr.Nodes[0].Text[0]) {
		t.Fatalf("nfd hash does not match nfd text")
	}
}

func TestInline_HashAndMeta(t *testing.T) {
	h := newTestRouter(t)
	urn := "urn:cts:greekLit:tlg0016.tlg001.grc:1.1"

	// Without flags: no hash, no meta (backward compatible).
	plain := getNodes(t, h, "/texts/"+urn)
	if plain.Nodes[0].Hash != "" || plain.Meta != nil {
		t.Fatalf("plain response should carry no hash/meta: hash=%q meta=%v", plain.Nodes[0].Hash, plain.Meta)
	}

	// With flags: per-node hash matches the /hash endpoint; meta from catalog.
	enriched := getNodes(t, h, "/texts/"+urn+"?hash=true&meta=true")
	hr := getHash(t, h, "/texts/hash/"+urn)
	if enriched.Nodes[0].Hash == "" || enriched.Nodes[0].Hash != hr.Hashes[0].Hash {
		t.Fatalf("inline hash %q != endpoint hash %q", enriched.Nodes[0].Hash, hr.Hashes[0].Hash)
	}
	m := enriched.Meta
	if m == nil || m.GroupName != "Herodotus" || m.WorkTitle != "Histories" || m.VersionLabel != "Greek" {
		t.Fatalf("meta wrong: %+v", m)
	}
}
