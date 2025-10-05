package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	cite "github.com/ThomasK81/gocite"
)

// --------- small util funcs shared across handlers ---------

func neighboursIDsIn(ids []string, i int) (prev, next string) {
	if i > 0 {
		prev = ids[i-1]
	}
	if i+1 < len(ids) {
		next = ids[i+1]
	}
	return
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

func idsFromWork(w cite.Work) []string {
	out := make([]string, len(w.Passages))
	for i := range w.Passages {
		out[i] = w.Passages[i].PassageID
	}
	return out
}

func textForID(id string, urns, texts []string) string {
	i := indexOf(urns, id)
	if i >= 0 {
		return texts[i]
	}
	return ""
}

func indexOf(xs []string, want string) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

func buildWorkForStem(allURNs []string, stem string) cite.Work {
	var w cite.Work
	w.WorkID = stem
	seq := 0
	for i := range allURNs {
		if strings.HasPrefix(allURNs[i], stem) {
			seq++
			w.Passages = append(w.Passages, cite.Passage{
				PassageID: allURNs[i],
				Index:     seq,
			})
		}
	}
	w, _ = cite.SortPassages(w)
	return w
}

func attachNeighbors(n *Node, ids []string, idx int) {
	prevID, nextID := neighboursIDsIn(ids, idx)
	if prevID != "" {
		n.Previous = []string{prevID}
	}
	if nextID != "" {
		n.Next = []string{nextID}
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
	at := strings.LastIndex(u, "@")
	if at < 0 {
		return "", "", 0, false
	}
	base := u[:at]
	rest := u[at+1:]
	occ := 1
	needle := rest
	if lb := strings.LastIndex(rest, "["); lb >= 0 && strings.HasSuffix(rest, "]") {
		needle = rest[:lb]
		nStr := rest[lb+1 : len(rest)-1]
		if n, err := strconv.Atoi(strings.TrimSpace(nStr)); err == nil && n >= 1 {
			occ = n
		}
	}
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return "", "", 0, false
	}
	return base, needle, occ, true
}

// n-th case-insensitive occurrence → rune start,end
func findNthInsensitive(haystack, needle string, n int) (int, int) {
	if n < 1 || needle == "" {
		return -1, -1
	}
	hl := strings.ToLower(haystack)
	nl := strings.ToLower(needle)
	bytePos := 0
	for i := 0; i < n; i++ {
		idx := strings.Index(hl[bytePos:], nl)
		if idx < 0 {
			return -1, -1
		}
		bytePos += idx
		if i < n-1 {
			bytePos += len(nl)
		}
	}
	runes := []rune(haystack)
	cb := 0
	startRune := 0
	for i, r := range runes {
		cb += len(string(r))
		if cb > bytePos {
			startRune = i
			break
		}
	}
	endRune := startRune + len([]rune(needle))
	if endRune > len(runes) {
		endRune = len(runes)
	}
	return startRune, endRune
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
