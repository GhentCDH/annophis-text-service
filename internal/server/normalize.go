package server

import (
	"net/http"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// normalizeMode reads the ?normalize= query parameter and reports whether it is
// supported. An absent parameter defaults to "nfc" (the stored form).
// Normalization is applied to response text only; the stored CEX data is never
// mutated.
func normalizeMode(r *http.Request) (mode string, ok bool) {
	m := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("normalize")))
	switch m {
	case "":
		return "nfc", true // default
	case "nfc", "nfd", "nfkc", "nfkd", "strip":
		return m, true
	default:
		return m, false
	}
}

// normalizeText applies a normalization mode to s.
func normalizeText(mode, s string) string {
	switch mode {
	case "nfc":
		return norm.NFC.String(s)
	case "nfd":
		return norm.NFD.String(s)
	case "nfkc":
		return norm.NFKC.String(s)
	case "nfkd":
		return norm.NFKD.String(s)
	case "strip":
		return stripToBase(s)
	default:
		return s
	}
}

// ligatureExpander expands letter ligatures that Unicode compatibility
// normalization does NOT decompose (they are encoded as distinct letters, not
// compatibility characters). Typographic ligatures such as ﬁ/ﬀ are handled by
// NFKD in the strip chain, so they are not listed here.
var ligatureExpander = strings.NewReplacer(
	"œ", "oe", "Œ", "OE",
	"æ", "ae", "Æ", "AE",
	"ß", "ss",
)

// stripBaseChain decomposes (NFKD, which also expands typographic ligatures),
// drops combining marks (Unicode Mn), then recomposes (NFC) to yield a
// base-character sequence with diacritics removed.
var stripBaseChain = transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// stripToBase produces a "plain" form: ligatures expanded and diacritics
// stripped — suitable for search, collation, and alignment.
func stripToBase(s string) string {
	s = ligatureExpander.Replace(s)
	out, _, err := transform.String(stripBaseChain, s)
	if err != nil {
		return s
	}
	return out
}

// writeNodes writes a node response, applying the request's ?normalize= mode to
// each node's text. The stored text is already NFC, so nfc is a no-op.
// Exception responses (no nodes) pass through unchanged.
func writeNodes(w http.ResponseWriter, r *http.Request, status int, resp NodeResponse) {
	mode, _ := normalizeMode(r) // validated by the handler; defaults to nfc
	if mode != "nfc" {
		for i := range resp.Nodes {
			for j := range resp.Nodes[i].Text {
				resp.Nodes[i].Text[j] = normalizeText(mode, resp.Nodes[i].Text[j])
			}
		}
	}
	writeJSON(w, status, resp)
}
