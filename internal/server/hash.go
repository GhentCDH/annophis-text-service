package server

import (
	"context"
	"crypto/sha1" //nolint:gosec // SHA-1 is used as a content fingerprint, not for security.
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// hashAlgo reads the ?algorithm= query parameter and reports whether it is
// supported. An absent parameter defaults to "sha1".
func hashAlgo(r *http.Request) (algo string, ok bool) {
	a := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("algorithm")))
	switch a {
	case "":
		return "sha1", true // default
	case "sha1", "sha256":
		return a, true
	default:
		return a, false
	}
}

// hashHex returns the lowercase hex digest of s under algo. The recipe is
// hex(algo(UTF-8(text))); callers pass the already-normalized text.
func hashHex(algo, s string) string {
	switch algo {
	case "sha256":
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	default: // sha1
		sum := sha1.Sum([]byte(s)) //nolint:gosec // fingerprint, not security.
		return hex.EncodeToString(sum[:])
	}
}

// sourceMeta derives provenance for a resolved passage from the CEX source and
// its catalog entry. Never fails: on a missing/unreadable catalog it returns
// only the source.
func (s *Server) sourceMeta(ctx context.Context, source string, nodes []Node) *SourceMeta {
	m := &SourceMeta{Source: source}
	if len(nodes) == 0 || len(nodes[0].URN) == 0 {
		return m
	}
	entries, err := s.parseCTSCatalog(ctx, source)
	if err != nil {
		return m
	}
	nodeURN := nodes[0].URN[0]
	for _, e := range entries {
		if e.URN != "" && strings.HasPrefix(nodeURN, e.URN) {
			m.GroupName = e.GroupName
			m.WorkTitle = e.WorkTitle
			m.VersionLabel = e.VersionLabel
			break
		}
	}
	return m
}

// handleHash serves GET /texts/hash/{URN}: a per-node content hash of the
// resolved text (same resolution as /texts/{URN}), computed over the normalized
// form (nfc default; honors ?normalize=). Algorithm defaults to sha1.
func (s *Server) handleHash(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cexName := chi.URLParam(r, "CEX")
	source := pickSourceFromReq(s.cfg, cexName, r.URL.Query())
	reqURN := chi.URLParam(r, "URN")
	svc := "/texts/hash"

	if _, ok := normalizeMode(r); !ok {
		writeJSON(w, http.StatusBadRequest, HashResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
			Message: "Unsupported normalize value. Use one of: nfc, nfd, nfkc, nfkd, strip.",
		})
		return
	}
	algo, ok := hashAlgo(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, HashResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
			Message: "Unsupported algorithm value. Use one of: sha1, sha256.",
		})
		return
	}

	allURNs, allTexts, err := s.parseCTSData(ctx, source)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, HashResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "No results for " + reqURN,
		})
		return
	}

	nodes, errResp, errStatus := s.resolveNodes(r, allURNs, allTexts, reqURN, svc)
	if errResp != nil {
		writeJSON(w, errStatus, HashResponse{
			RequestUrn: errResp.RequestUrn, Status: errResp.Status, Service: errResp.Service, Message: errResp.Message,
		})
		return
	}

	mode, _ := normalizeMode(r)
	applyNormalization(mode, nodes)

	hashes := make([]NodeHash, 0, len(nodes))
	for _, n := range nodes {
		urn := ""
		if len(n.URN) > 0 {
			urn = n.URN[0]
		}
		hashes = append(hashes, NodeHash{URN: urn, Hash: hashHex(algo, strings.Join(n.Text, ""))})
	}

	writeJSON(w, http.StatusOK, HashResponse{
		RequestUrn: []string{reqURN}, Status: "Success", Service: svc, Algorithm: algo, Hashes: hashes,
	})
}
