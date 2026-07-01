package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	cite "github.com/ThomasK81/gocite"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handlePassage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cexName := chi.URLParam(r, "CEX")
	source := pickSourceFromReq(s.cfg, cexName, r.URL.Query())
	reqURN := chi.URLParam(r, "URN")
	svc := "/texts"

	if _, ok := normalizeMode(r); !ok {
		writeJSON(w, http.StatusBadRequest, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
			Message: "Unsupported normalize value. Use one of: nfc, nfd, nfkc, nfkd, strip.",
		})
		return
	}
	if _, ok := hashAlgo(r); parseBool(r.URL.Query().Get("hash")) && !ok {
		writeJSON(w, http.StatusBadRequest, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
			Message: "Unsupported algorithm value. Use one of: sha1, sha256.",
		})
		return
	}

	allURNs, allTexts, err := s.loadCorpus(ctx, source)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "No results for " + reqURN,
		})
		return
	}

	nodes, errResp, errStatus := s.resolveNodes(r, allURNs, allTexts, reqURN, svc)
	if errResp != nil {
		writeJSON(w, errStatus, *errResp)
		return
	}

	// Normalize the resolved text (nfc default) before hashing/serving so the
	// returned text and its hash always correspond.
	mode, _ := normalizeMode(r)
	applyNormalization(mode, nodes)

	if parseBool(r.URL.Query().Get("hash")) {
		algo, _ := hashAlgo(r)
		for i := range nodes {
			nodes[i].Hash = hashHex(algo, strings.Join(nodes[i].Text, ""))
		}
	}

	resp := NodeResponse{RequestUrn: []string{reqURN}, Status: "Success", Service: svc, Nodes: nodes}
	if parseBool(r.URL.Query().Get("meta")) {
		resp.Meta = s.sourceMeta(ctx, source, nodes)
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolveNodes resolves reqURN (exact / prefix / range / anchored) to its result
// nodes, applying the request's text filters. On success it returns the nodes
// (with NFC-baseline text, before any ?normalize= transform). On failure it
// returns a ready-to-write exception response and its HTTP status; nodes is nil.
func (s *Server) resolveNodes(r *http.Request, allURNs, allTexts []string, reqURN, svc string) ([]Node, *NodeResponse, int) {
	// --- Anchored (single)
	if cite.WantSubstr(reqURN) && !cite.IsRange(reqURN) {
		baseURN, needle, occ, ok := parseAnchoredURN(reqURN)
		if !ok {
			return nil, &NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Malformed anchored URN.",
			}, http.StatusBadRequest
		}
		if !cite.IsCTSURN(baseURN) {
			return nil, &NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: baseURN + " is not valid CTS.",
			}, http.StatusBadRequest
		}
		idx := indexOf(allURNs, baseURN)
		if idx < 0 {
			return nil, &NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not find base passage " + baseURN,
			}, http.StatusOK
		}
		full := allTexts[idx]

		var textOut string
		var complete bool
		if strings.HasPrefix(needle, "/") && strings.HasSuffix(needle, "/") {
			pat := strings.TrimSuffix(strings.TrimPrefix(needle, "/"), "/")
			re, err := regexp.Compile("(?i)" + pat)
			if err != nil {
				return nil, &NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Invalid regex pattern.",
				}, http.StatusBadRequest
			}
			matches := re.FindAllStringIndex(full, -1)
			if occ < 1 || occ > len(matches) {
				return nil, &NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
					Message: fmt.Sprintf("Regex %q (occurrence %d) not found in %s.", pat, occ, baseURN),
				}, http.StatusOK
			}
			start := matches[occ-1][0]
			end := matches[occ-1][1]
			textOut, complete = anchorWindowFromByteOffsets(r, full, start, end)
		} else {
			startRune, endRune := findAnchorMatch(r, full, needle, occ)
			if startRune < 0 {
				return nil, &NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
					Message: fmt.Sprintf("Substring %q (occurrence %d) not found in %s.", needle, occ, baseURN),
				}, http.StatusOK
			}
			rns := []rune(full)
			textOut, complete = anchorWindowFromRuneOffsets(r, rns, startRune, endRune)
		}

		node := Node{
			URN:      []string{baseURN},
			Text:     []string{textOut},
			Sequence: sequenceWithinWork(allURNs, idx),
			Complete: complete,
		}
		attachNeighbors(&node, allURNs, idx)
		return []Node{node}, nil, 0
	}

	// Validate CTS/range
	if !cite.IsCTSURN(reqURN) && !cite.IsRange(reqURN) {
		return nil, &NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: reqURN + " is not valid CTS.",
		}, http.StatusBadRequest
	}

	// --- Exact node
	if idx := indexOf(allURNs, reqURN); idx >= 0 {
		txt := allTexts[idx]
		txt, complete := applyTextFilters(r, txt)
		node := Node{
			URN:      []string{allURNs[idx]},
			Text:     []string{txt},
			Sequence: sequenceWithinWork(allURNs, idx),
			Complete: complete,
		}
		attachNeighbors(&node, allURNs, idx)
		return []Node{node}, nil, 0
	}

	// --- Prefix expansion (non-range)
	if !cite.IsRange(reqURN) {
		var nodes []Node
		for i, id := range allURNs {
			if strings.HasPrefix(id, reqURN) {
				txt, complete := applyTextFilters(r, allTexts[i])
				n := Node{
					URN:      []string{id},
					Text:     []string{txt},
					Sequence: i + 1,
					Complete: complete,
				}
				attachNeighbors(&n, allURNs, i)
				nodes = append(nodes, n)
			}
		}
		if len(nodes) == 0 {
			return nil, &NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not find node to " + reqURN + " in source.",
			}, http.StatusOK
		}
		return nodes, nil, 0
	}

	// --- Range (supports anchors on both sides)
	parts := strings.Split(reqURN, ":")
	if len(parts) < 5 {
		return nil, &NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not parse " + reqURN,
		}, http.StatusOK
	}
	stem := strings.Join(parts[:4], ":") + ":"
	rangeRef := parts[4]
	dash := strings.Index(rangeRef, "-")
	if dash <= 0 || dash >= len(rangeRef)-1 {
		return nil, &NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not parse range " + reqURN,
		}, http.StatusOK
	}
	leftTok := rangeRef[:dash]
	rightTok := rangeRef[dash+1:]

	lRef, lNeedle, lOcc, lAnch := parseRefAnchorToken(leftTok)
	rRef, rNeedle, rOcc, rAnch := parseRefAnchorToken(rightTok)
	if rAnch && rRef == "" {
		rRef = lRef
	}

	// filter to this stem
	var fURNs, fTexts []string
	for i, id := range allURNs {
		if strings.HasPrefix(id, stem) {
			fURNs = append(fURNs, id)
			fTexts = append(fTexts, allTexts[i])
		}
	}
	if len(fURNs) == 0 {
		return nil, &NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not find node to " + reqURN + " in source.",
		}, http.StatusOK
	}

	startID := stem + lRef
	endID := stem + rRef

	sIdx := indexOf(fURNs, startID)
	if sIdx < 0 && lRef != "" {
		sIdx = firstPrefixIndex(fURNs, startID)
	}
	eIdx := indexOf(fURNs, endID)
	if eIdx < 0 && rRef != "" {
		eIdx = firstPrefixIndex(fURNs, endID)
	}

	// both anchors in same passage
	if lAnch && rAnch && rRef == lRef && sIdx >= 0 {
		full := fTexts[sIdx]
		startRune, endRuneStart := findAnchorMatch(r, full, lNeedle, lOcc)
		if startRune < 0 {
			return nil, &NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
				Message: fmt.Sprintf("Start anchor %q (occurrence %d) not found in %s.", lNeedle, lOcc, stem+lRef),
			}, http.StatusOK
		}
		erS, erE := findAnchorMatch(r, full, rNeedle, rOcc)
		if erS < 0 || erS < endRuneStart {
			return nil, &NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
				Message: fmt.Sprintf("End anchor %q (occurrence %d) not found after start in %s.", rNeedle, rOcc, stem+lRef),
			}, http.StatusOK
		}
		rns := []rune(full)
		txt, complete := sliceBetweenRunes(rns, startRune, erE)
		node := Node{
			URN:      []string{fURNs[sIdx]},
			Text:     []string{txt},
			Sequence: sIdx + 1,
			Complete: complete,
		}
		attachNeighbors(&node, fURNs, sIdx)
		return []Node{node}, nil, 0
	}

	if sIdx < 0 {
		return nil, &NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Start of range not found.",
		}, http.StatusOK
	}
	if rRef != "" && eIdx < 0 {
		return nil, &NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "End of range not found.",
		}, http.StatusOK
	}
	if !rAnch && rRef == "" {
		return nil, &NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Right side of range missing.",
		}, http.StatusOK
	}
	if eIdx >= 0 && sIdx > eIdx {
		sIdx, eIdx = eIdx, sIdx
		lAnch, rAnch = rAnch, lAnch
		lNeedle, rNeedle = rNeedle, lNeedle
		lOcc, rOcc = rOcc, lOcc
	}

	var nodes []Node

	// Start
	{
		txt := fTexts[sIdx]
		if lAnch {
			sr, _ := findAnchorMatch(r, txt, lNeedle, lOcc)
			if sr < 0 {
				return nil, &NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
					Message: fmt.Sprintf("Start anchor %q (occurrence %d) not found in %s.", lNeedle, lOcc, fURNs[sIdx]),
				}, http.StatusOK
			}
			rns := []rune(txt)
			out, complete := sliceFromRunes(rns, sr)
			n := Node{
				URN:      []string{fURNs[sIdx]},
				Text:     []string{out},
				Sequence: sIdx + 1,
				Complete: complete,
			}
			attachNeighbors(&n, fURNs, sIdx)
			nodes = append(nodes, n)
		} else {
			out, complete := applyTextFilters(r, txt)
			n := Node{
				URN:      []string{fURNs[sIdx]},
				Text:     []string{out},
				Sequence: sIdx + 1,
				Complete: complete,
			}
			attachNeighbors(&n, fURNs, sIdx)
			nodes = append(nodes, n)
		}
	}

	// Middles
	if eIdx >= 0 {
		for i := sIdx + 1; i < eIdx; i++ {
			out, complete := applyTextFilters(r, fTexts[i])
			n := Node{
				URN:      []string{fURNs[i]},
				Text:     []string{out},
				Sequence: i + 1,
				Complete: complete,
			}
			attachNeighbors(&n, fURNs, i)
			nodes = append(nodes, n)
		}
	}

	// End
	if eIdx >= 0 && eIdx >= sIdx {
		txt := fTexts[eIdx]
		if rAnch {
			erS, erE := findAnchorMatch(r, txt, rNeedle, rOcc)
			if erS < 0 {
				return nil, &NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
					Message: fmt.Sprintf("End anchor %q (occurrence %d) not found in %s.", rNeedle, rOcc, fURNs[eIdx]),
				}, http.StatusOK
			}
			rns := []rune(txt)
			out, complete := sliceUntilRunes(rns, erE)
			n := Node{
				URN:      []string{fURNs[eIdx]},
				Text:     []string{out},
				Sequence: eIdx + 1,
				Complete: complete,
			}
			attachNeighbors(&n, fURNs, eIdx)
			nodes = append(nodes, n)
		} else if eIdx != sIdx {
			out, complete := applyTextFilters(r, txt)
			n := Node{
				URN:      []string{fURNs[eIdx]},
				Text:     []string{out},
				Sequence: eIdx + 1,
				Complete: complete,
			}
			attachNeighbors(&n, fURNs, eIdx)
			nodes = append(nodes, n)
		}
	}

	return nodes, nil, 0
}

func (s *Server) handleNavFirst(w http.ResponseWriter, r *http.Request) {
	s.handleNav(w, r, "first")
}

func (s *Server) handleNavLast(w http.ResponseWriter, r *http.Request) {
	s.handleNav(w, r, "last")
}

func (s *Server) handleNavPrevious(w http.ResponseWriter, r *http.Request) {
	s.handleNav(w, r, "previous")
}

func (s *Server) handleNavNext(w http.ResponseWriter, r *http.Request) {
	s.handleNav(w, r, "next")
}

func (s *Server) handleNav(w http.ResponseWriter, r *http.Request, nav string) {
	ctx := r.Context()
	cexName := chi.URLParam(r, "CEX")
	source := pickSourceFromReq(s.cfg, cexName, r.URL.Query())
	reqURN := chi.URLParam(r, "URN")
	svc := "/texts"

	if _, ok := normalizeMode(r); !ok {
		writeJSON(w, http.StatusBadRequest, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
			Message: "Unsupported normalize value. Use one of: nfc, nfd, nfkc, nfkd, strip.",
		})
		return
	}

	// validate URN
	if !cite.IsCTSURN(reqURN) {
		writeJSON(w, http.StatusBadRequest, NodeResponse{
			RequestUrn: []string{reqURN},
			Status:     "Exception",
			Service:    svc,
			Message:    reqURN + " is not valid CTS.",
		})
		return
	}

	// Load data
	allURNs, allTexts, err := s.loadCorpus(ctx, source)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, NodeResponse{
			RequestUrn: []string{reqURN},
			Status:     "Exception",
			Service:    svc,
			Message:    "No results for " + reqURN,
		})
		return
	}

	var idx = -1

	switch nav {
	case "first":
		stem := workStem(reqURN)
		for i, u := range allURNs {
			if strings.HasPrefix(u, stem) {
				idx = i
				break
			}
		}
	case "last":
		stem := workStem(reqURN)
		for i := len(allURNs) - 1; i >= 0; i-- {
			if strings.HasPrefix(allURNs[i], stem) {
				idx = i
				break
			}
		}
	case "previous":
		cur := indexOf(allURNs, reqURN)
		if cur < 0 {
			writeNodes(w, r, http.StatusOK, NodeResponse{
				RequestUrn: []string{reqURN},
				Status:     "Exception",
				Service:    svc,
				Message:    "Current passage not found.",
			})
			return
		}
		if cur == 0 || !sameWork(allURNs[cur-1], reqURN) {
			// no previous in same work
			writeNodes(w, r, http.StatusOK, NodeResponse{
				RequestUrn: []string{reqURN},
				Status:     "Exception",
				Service:    svc,
				Message:    "No previous passage in this work.",
			})
			return
		}
		idx = cur - 1

	case "next":
		cur := indexOf(allURNs, reqURN)
		if cur < 0 {
			writeNodes(w, r, http.StatusOK, NodeResponse{
				RequestUrn: []string{reqURN},
				Status:     "Exception",
				Service:    svc,
				Message:    "Current passage not found.",
			})
			return
		}
		if cur+1 >= len(allURNs) || !sameWork(allURNs[cur+1], reqURN) {
			// no next in same work
			writeNodes(w, r, http.StatusOK, NodeResponse{
				RequestUrn: []string{reqURN},
				Status:     "Exception",
				Service:    svc,
				Message:    "No next passage in this work.",
			})
			return
		}
		idx = cur + 1
	}

	if idx < 0 || idx >= len(allURNs) {
		writeNodes(w, r, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN},
			Status:     "Exception",
			Service:    svc,
			Message:    "Navigation target not found.",
		})
		return
	}

	txt := allTexts[idx]
	txt, complete := applyTextFilters(r, txt)

	node := Node{
		URN:      []string{allURNs[idx]},
		Text:     []string{txt},
		Sequence: sequenceWithinWork(allURNs, idx),
		Complete: complete,
	}
	attachNeighbors(&node, allURNs, idx)

	writeNodes(w, r, http.StatusOK, NodeResponse{
		RequestUrn: []string{reqURN},
		Status:     "Success",
		Service:    svc,
		Nodes:      []Node{node},
	})
}
