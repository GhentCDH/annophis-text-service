package server

import (
	"golang.org/x/text/unicode/norm"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"
)

// urnParam returns the {URN} path parameter, percent-decoded. Clients may encode
// the URN (e.g. %3A for ':', %2F for '/' in regex anchors); chi does not decode
// path params, so we do it here — once — at the handler boundary.
func urnParam(r *http.Request) string {
	raw := chi.URLParam(r, "URN")
	if dec, err := url.PathUnescape(raw); err == nil {
		return dec
	}
	return raw
}

// workStem returns the 4-part CTS work stem with trailing colon,
// e.g. urn:cts:greekLit:tlg0007.tlg012.ziegler:
func workStem(u string) string {
	parts := strings.Split(u, ":")
	if len(parts) < 4 {
		return u
	}
	return strings.Join(parts[:4], ":") + ":"
}

// sameWork reports whether two URNs belong to the same work stem.
func sameWork(a, b string) bool {
	return strings.HasPrefix(a, workStem(b))
}

// sequenceWithinWork returns a 1-based index of ids[idx] within its work.
func sequenceWithinWork(ids []string, idx int) int {
	if idx < 0 || idx >= len(ids) {
		return 0
	}
	stem := workStem(ids[idx])
	seq := 0
	for i := 0; i <= idx; i++ {
		if strings.HasPrefix(ids[i], stem) {
			seq++
		}
	}
	return seq
}

func firstPrefixIndex(ids []string, prefix string) int {
	for i, id := range ids {
		if strings.HasPrefix(id, prefix) {
			return i
		}
	}
	return -1
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "t", "true", "y", "yes":
		return true
	default:
		return false
	}
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}

// Keep first-occurrence order
func dedupPreserveOrder(xs []string) []string {
	seen := make(map[string]struct{}, len(xs))
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}

func indexOf(xs []string, want string) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

func attachNeighbors(n *Node, ids []string, idx int) {
	if idx < 0 || idx >= len(ids) {
		return
	}
	current := ids[idx]

	// previous in same work only
	if idx > 0 && sameWork(ids[idx-1], current) {
		n.Previous = []string{ids[idx-1]}
	}

	// next in same work only
	if idx+1 < len(ids) && sameWork(ids[idx+1], current) {
		n.Next = []string{ids[idx+1]}
	}
}

// ------------- text clipping / anchors (no ellipses) -------------

func applyTextFilters(r *http.Request, full string) (string, bool) {
	q := r.URL.Query()
	substr := strings.TrimSpace(q.Get("substring"))
	clip := parseBool(q.Get("clip"))
	context := parseIntDefault(q.Get("context"), 40)
	maxChars := parseIntDefault(q.Get("maxChars"), 0)

	out := full
	complete := true

	if substr != "" && clip {
		out2, ok := clipToSubstring(full, substr, context)
		out = out2
		if !ok || out2 != full {
			complete = false
		}
	}
	if maxChars > 0 {
		rr := []rune(out)
		if len(rr) > maxChars {
			out = string(rr[:maxChars])
			complete = false
		}
	}
	return out, complete
}

func clipToSubstring(full, needle string, ctx int) (string, bool) {
	if needle == "" {
		return full, true
	}
	lowerFull := strings.ToLower(full)
	lowerNeedle := strings.ToLower(needle)
	bi := strings.Index(lowerFull, lowerNeedle)
	if bi < 0 {
		return full, true
	}
	rns := []rune(full)
	cb := 0
	startRune := 0
	for i, r := range rns {
		cb += len(string(r))
		if cb > bi {
			startRune = i
			break
		}
	}
	endRune := startRune + len([]rune(needle))

	s := startRune - ctx
	if s < 0 {
		s = 0
	}
	e := endRune + ctx
	if e > len(rns) {
		e = len(rns)
	}
	out := string(rns[s:e])
	complete := (s == 0 && e == len(rns))
	return out, complete
}

// byte -> rune window around match; returns (text, complete)
func anchorWindowFromByteOffsets(r *http.Request, full string, startByte, endByte int) (string, bool) {
	rns := []rune(full)
	cb := 0
	s := 0
	for i, rr := range rns {
		cb += len(string(rr))
		if cb > startByte {
			s = i
			break
		}
	}
	cb2 := 0
	e := s
	for i := s; i < len(rns); i++ {
		cb2 += len(string(rns[i]))
		if cb2 >= (endByte - startByte) {
			e = i + 1
			break
		}
	}
	return anchorWindowFromRuneOffsets(r, rns, s, e)
}

// builds snippet/full around rune offsets; never adds ellipses
func anchorWindowFromRuneOffsets(r *http.Request, rns []rune, startRune, endRune int) (string, bool) {
	q := r.URL.Query()

	clip := true // default clip for anchors
	if v := q.Get("clip"); v != "" {
		clip = parseBool(v)
	}

	// tail: from match to end of passage
	if parseBool(q.Get("tail")) {
		out := string(rns[startRune:])
		complete := (startRune == 0)
		if maxChars := parseIntDefault(q.Get("maxChars"), 0); maxChars > 0 {
			rr := []rune(out)
			if len(rr) > maxChars {
				return string(rr[:maxChars]), false
			}
		}
		return out, complete
	}

	ctx := parseIntDefault(q.Get("context"), 0)
	maxChars := parseIntDefault(q.Get("maxChars"), 0)

	if !clip && ctx == 0 {
		txt := string(rns)
		if maxChars > 0 {
			rr := []rune(txt)
			if len(rr) > maxChars {
				return string(rr[:maxChars]), false
			}
		}
		return txt, true
	}

	s := startRune - ctx
	if s < 0 {
		s = 0
	}
	e := endRune + ctx
	if e > len(rns) {
		e = len(rns)
	}
	out := string(rns[s:e])
	complete := (s == 0 && e == len(rns))

	if maxChars > 0 {
		rr := []rune(out)
		if len(rr) > maxChars {
			return string(rr[:maxChars]), false
		}
	}
	return out, complete
}

// "urn:...:<ref>@needle[n]" → (base, needle, occ, ok)
func parseAnchoredURN(u string) (string, string, int, bool) {
	// u is already percent-decoded by urnParam at the handler boundary.
	at := strings.LastIndex(u, "@")
	if at < 0 {
		return "", "", 0, false
	}

	base := strings.TrimSpace(u[:at])
	rest := strings.TrimSpace(u[at+1:])

	if rest == "" {
		return "", "", 0, false
	}

	occ := 1
	needle := rest

	// optional [n] suffix
	if lb := strings.LastIndex(rest, "["); lb >= 0 && strings.HasSuffix(rest, "]") {
		nStr := rest[lb+1 : len(rest)-1]
		if n, err := strconv.Atoi(strings.TrimSpace(nStr)); err == nil && n >= 1 {
			occ = n
		}
		needle = rest[:lb]
	}

	needle = strings.TrimSpace(needle)
	if needle == "" {
		return "", "", 0, false
	}

	// Normalise both base and needle so they match the NFC text we stored
	base = norm.NFC.String(base)
	needle = norm.NFC.String(needle)

	return base, needle, occ, true
}

// findAnchorMatch resolves the n-th occurrence of needle in haystack, returning
// rune start,end offsets in the ORIGINAL string. Matching is always
// case-insensitive; when the request carries ?ignoreAccents=true it is also
// diacritic-insensitive, so e.g. "Περσαι" matches "Πέρσαι".
func findAnchorMatch(r *http.Request, haystack, needle string, n int) (int, int) {
	if parseBool(r.URL.Query().Get("ignoreAccents")) {
		return findNthFolded(haystack, needle, n)
	}
	return findNthInsensitive(haystack, needle, n)
}

// buildFolded returns a diacritic-stripped copy of rns (via NFD decomposition,
// dropping combining marks) together with origIdx, mapping each folded rune back
// to the index of the original rune it came from. This lets accent-insensitive
// matches be reported as offsets into the original, un-stripped text.
func buildFolded(rns []rune) (folded []rune, origIdx []int) {
	for i, r := range rns {
		for _, dr := range norm.NFD.String(string(r)) {
			if unicode.Is(unicode.Mn, dr) { // nonspacing (combining) mark
				continue
			}
			folded = append(folded, dr)
			origIdx = append(origIdx, i)
		}
	}
	return folded, origIdx
}

// stripDiacritics removes combining marks from s (NFD, drop Mn), preserving case.
func stripDiacritics(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// findNthFolded is like findNthInsensitive but ignores diacritics on both sides.
// Offsets are mapped back to the original (accented) rune positions.
func findNthFolded(haystack, needle string, n int) (int, int) {
	if n < 1 || needle == "" {
		return -1, -1
	}
	hr := []rune(haystack)
	folded, origIdx := buildFolded(hr)
	nf := stripDiacritics(needle)

	hl := len(folded)
	nl := len([]rune(nf))
	if nl == 0 || nl > hl {
		return -1, -1
	}

	occ := 0
	for i := 0; i <= hl-nl; i++ {
		if strings.EqualFold(string(folded[i:i+nl]), nf) {
			occ++
			if occ == n {
				return origIdx[i], origIdx[i+nl-1] + 1
			}
		}
	}
	return -1, -1
}

// n-th case-insensitive occurrence → rune start,end
func findNthInsensitive(haystack, needle string, n int) (int, int) {
	if n < 1 || needle == "" {
		return -1, -1
	}

	hr := []rune(haystack)
	nr := []rune(needle)

	hl := len(hr)
	nl := len(nr)

	if nl == 0 || nl > hl {
		return -1, -1
	}

	occ := 0

	for i := 0; i <= hl-nl; i++ {
		sub := string(hr[i : i+nl])
		if strings.EqualFold(sub, needle) { // Unicode-aware case-fold
			occ++
			if occ == n {
				// i is startRune, i+nl is endRune in the ORIGINAL string
				return i, i + nl
			}
		}
	}

	return -1, -1
}

// range tokens like "1.0@foo[1]" → (ref, needle, occ, anchored)
func parseRefAnchorToken(tok string) (ref, needle string, occ int, anchored bool) {
	occ = 1
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", "", 1, false
	}

	at := strings.Index(tok, "@")
	if at < 0 {
		return tok, "", 1, false
	}
	ref = strings.TrimSpace(tok[:at])
	anchored = true
	rest := strings.TrimSpace(tok[at+1:])
	if lb := strings.LastIndex(rest, "["); lb >= 0 && strings.HasSuffix(rest, "]") {
		needle = strings.TrimSpace(rest[:lb])
		nStr := rest[lb+1 : len(rest)-1]
		if n, err := strconv.Atoi(strings.TrimSpace(nStr)); err == nil && n >= 1 {
			occ = n
		}
	} else {
		needle = rest
	}

	// Normalise ref and needle to NFC so range anchors match the NFC text we
	// stored, mirroring parseAnchoredURN for single anchors.
	ref = norm.NFC.String(ref)
	needle = norm.NFC.String(needle)

	return ref, needle, occ, anchored
}

func sliceFromRunes(rns []rune, start int) (string, bool) {
	if start < 0 {
		start = 0
	}
	if start > len(rns) {
		start = len(rns)
	}
	out := string(rns[start:])
	complete := (start == 0)
	return out, complete
}
func sliceUntilRunes(rns []rune, end int) (string, bool) {
	if end < 0 {
		end = 0
	}
	if end > len(rns) {
		end = len(rns)
	}
	out := string(rns[:end])
	complete := (end == len(rns))
	return out, complete
}
func sliceBetweenRunes(rns []rune, start, end int) (string, bool) {
	if start < 0 {
		start = 0
	}
	if end > len(rns) {
		end = len(rns)
	}
	if start > end {
		start, end = end, start
	}
	out := string(rns[start:end])
	complete := (start == 0 && end == len(rns))
	return out, complete
}

// local pickSource wrapper so handlers can use it
func pickSourceFromReq(cfg ServerConfig, cex string, q url.Values) string {
	return pickSource(cfg, cex, q)
}
