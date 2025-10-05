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

	// Load data
	allURNs, allTexts, err := s.parseCTSData(ctx, source)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "No results for " + reqURN,
		})
		return
	}

	// --- Anchored (single)
	if cite.WantSubstr(reqURN) && !cite.IsRange(reqURN) {
		baseURN, needle, occ, ok := parseAnchoredURN(reqURN)
		if !ok {
			writeJSON(w, http.StatusBadRequest, NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Malformed anchored URN.",
			})
			return
		}
		if !cite.IsCTSURN(baseURN) {
			writeJSON(w, http.StatusBadRequest, NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: baseURN + " is not valid CTS.",
			})
			return
		}
		idx := indexOf(allURNs, baseURN)
		if idx < 0 {
			writeJSON(w, http.StatusOK, NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not find base passage " + baseURN,
			})
			return
		}
		full := allTexts[idx]

		var textOut string
		var complete bool
		if strings.HasPrefix(needle, "/") && strings.HasSuffix(needle, "/") {
			pat := strings.TrimSuffix(strings.TrimPrefix(needle, "/"), "/")
			re, err := regexp.Compile("(?i)" + pat)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Invalid regex pattern.",
				})
				return
			}
			matches := re.FindAllStringIndex(full, -1)
			if occ < 1 || occ > len(matches) {
				writeJSON(w, http.StatusOK, NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
					Message: fmt.Sprintf("Regex %q (occurrence %d) not found in %s.", pat, occ, baseURN),
				})
				return
			}
			start := matches[occ-1][0]
			end := matches[occ-1][1]
			textOut, complete = anchorWindowFromByteOffsets(r, full, start, end)
		} else {
			startRune, endRune := findNthInsensitive(full, needle, occ)
			if startRune < 0 {
				writeJSON(w, http.StatusOK, NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
					Message: fmt.Sprintf("Substring %q (occurrence %d) not found in %s.", needle, occ, baseURN),
				})
				return
			}
			rns := []rune(full)
			textOut, complete = anchorWindowFromRuneOffsets(r, rns, startRune, endRune)
		}

		node := Node{
			URN:      []string{baseURN},
			Text:     []string{textOut},
			Sequence: idx + 1,
			Complete: complete,
		}
		attachNeighbors(&node, allURNs, idx)

		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Success", Service: svc, Nodes: []Node{node},
		})
		return
	}

	// Validate CTS/range
	if !cite.IsCTSURN(reqURN) && !cite.IsRange(reqURN) {
		writeJSON(w, http.StatusBadRequest, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: reqURN + " is not valid CTS.",
		})
		return
	}

	// --- Exact node
	if idx := indexOf(allURNs, reqURN); idx >= 0 {
		txt := allTexts[idx]
		txt, complete := applyTextFilters(r, txt)
		node := Node{
			URN:      []string{allURNs[idx]},
			Text:     []string{txt},
			Sequence: idx + 1,
			Complete: complete,
		}
		attachNeighbors(&node, allURNs, idx)
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Success", Service: svc, Nodes: []Node{node},
		})
		return
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
			writeJSON(w, http.StatusOK, NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not find node to " + reqURN + " in source.",
			})
			return
		}
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Success", Service: svc, Nodes: nodes,
		})
		return
	}

	// --- Range (supports anchors on both sides)
	parts := strings.Split(reqURN, ":")
	if len(parts) < 5 {
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not parse " + reqURN,
		})
		return
	}
	stem := strings.Join(parts[:4], ":") + ":"
	rangeRef := parts[4]
	dash := strings.Index(rangeRef, "-")
	if dash <= 0 || dash >= len(rangeRef)-1 {
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not parse range " + reqURN,
		})
		return
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
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Could not find node to " + reqURN + " in source.",
		})
		return
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
		startRune, endRuneStart := findNthInsensitive(full, lNeedle, lOcc)
		if startRune < 0 {
			writeJSON(w, http.StatusOK, NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
				Message: fmt.Sprintf("Start anchor %q (occurrence %d) not found in %s.", lNeedle, lOcc, stem+lRef),
			})
			return
		}
		erS, erE := findNthInsensitive(full, rNeedle, rOcc)
		if erS < 0 || erS < endRuneStart {
			writeJSON(w, http.StatusOK, NodeResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
				Message: fmt.Sprintf("End anchor %q (occurrence %d) not found after start in %s.", rNeedle, rOcc, stem+lRef),
			})
			return
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
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Success", Service: svc, Nodes: []Node{node},
		})
		return
	}

	if sIdx < 0 {
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Start of range not found.",
		})
		return
	}
	if rRef != "" && eIdx < 0 {
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "End of range not found.",
		})
		return
	}
	if !rAnch && rRef == "" {
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Right side of range missing.",
		})
		return
	}
	if eIdx >= 0 && sIdx > eIdx {
		sIdx, eIdx = eIdx, sIdx
		lAnch, rAnch = rAnch, lAnch
		lRef, rRef = rRef, lRef
		lNeedle, rNeedle = rNeedle, lNeedle
		lOcc, rOcc = rOcc, lOcc
	}

	var nodes []Node

	// Start
	{
		txt := fTexts[sIdx]
		if lAnch {
			sr, _ := findNthInsensitive(txt, lNeedle, lOcc)
			if sr < 0 {
				writeJSON(w, http.StatusOK, NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
					Message: fmt.Sprintf("Start anchor %q (occurrence %d) not found in %s.", lNeedle, lOcc, fURNs[sIdx]),
				})
				return
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
			erS, erE := findNthInsensitive(txt, rNeedle, rOcc)
			if erS < 0 {
				writeJSON(w, http.StatusOK, NodeResponse{
					RequestUrn: []string{reqURN}, Status: "Exception", Service: svc,
					Message: fmt.Sprintf("End anchor %q (occurrence %d) not found in %s.", rNeedle, rOcc, fURNs[eIdx]),
				})
				return
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

	writeJSON(w, http.StatusOK, NodeResponse{
		RequestUrn: []string{reqURN}, Status: "Success", Service: svc, Nodes: nodes,
	})
}
