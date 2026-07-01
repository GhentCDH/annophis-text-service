package server

import "testing"

// A percent-encoded URN (e.g. %3A for ':') must be accepted on every endpoint,
// not just the anchored passage path.
func TestURN_PercentEncoded(t *testing.T) {
	h := newTestRouter(t)
	enc := "urn%3Acts%3AgreekLit%3Atlg0016.tlg001.grc%3A1.1" // urn:cts:greekLit:tlg0016.tlg001.grc:1.1

	if ur := getURNs(t, h, "/texts/urns/"+enc); ur.Status != "Success" {
		t.Fatalf("/texts/urns: status=%s msg=%q", ur.Status, ur.Message)
	}
	if nr := getNodes(t, h, "/texts/"+enc); nr.Status != "Success" {
		t.Fatalf("/texts/{URN}: status=%s msg=%q", nr.Status, nr.Message)
	}
	if hr := getHash(t, h, "/texts/hash/"+enc); hr.Status != "Success" {
		t.Fatalf("/texts/hash: status=%s msg=%q", hr.Status, hr.Message)
	}
	if nr := getNodes(t, h, "/texts/next/"+enc); nr.Status != "Success" {
		t.Fatalf("/texts/next: status=%s msg=%q", nr.Status, nr.Message)
	}
	// requestUrn should echo the decoded URN, not the raw %3A form.
	if nr := getNodes(t, h, "/texts/"+enc); nr.RequestUrn[0] != "urn:cts:greekLit:tlg0016.tlg001.grc:1.1" {
		t.Fatalf("requestUrn not decoded: %q", nr.RequestUrn[0])
	}
}
