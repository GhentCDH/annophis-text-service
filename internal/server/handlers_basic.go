package server

import (
	"net/http"
	"strings"

	cite "github.com/ThomasK81/gocite"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleCiteVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, CITEResponse{
		Status:  "Success",
		Service: "/cite",
		Versions: Versions{
			Texts: "1.1.0",
		},
	})
}

func (s *Server) handleTextsVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, VersionResponse{
		Status:  "Success",
		Service: "/texts/version",
		Version: "1.1.0",
	})
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cexName := chi.URLParam(r, "CEX")
	source := pickSourceFromReq(s.cfg, cexName, r.URL.Query())

	entries, err := s.parseCTSCatalog(ctx, source)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, CatalogResponse{
			Status:  "Exception",
			Service: "/texts/catalog",
			Message: "Couldn't read catalog: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, CatalogResponse{
		Status:  "Success",
		Service: "/texts/catalog",
		Entries: entries,
	})
}

func (s *Server) handleWorkURNs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cexName := chi.URLParam(r, "CEX")
	source := pickSourceFromReq(s.cfg, cexName, r.URL.Query())

	urns, _, err := s.parseCTSData(ctx, source)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, URNResponse{
			Status:  "Exception",
			Service: "/texts",
			Message: "Couldn't open connection.",
		})
		return
	}
	stems := make([]string, 0, len(urns))
	for _, u := range urns {
		parts := strings.Split(u, ":")
		if len(parts) >= 4 {
			stems = append(stems, strings.Join(parts[:4], ":")+":")
		}
	}
	stems = dedupPreserveOrder(stems)

	writeJSON(w, http.StatusOK, URNResponse{
		RequestUrn: []string{},
		Status:     "Success",
		Service:    "/texts",
		URN:        stems,
	})
}

func (s *Server) handleFirst(w http.ResponseWriter, r *http.Request) { s.firstOrLast(w, r, true) }
func (s *Server) handleLast(w http.ResponseWriter, r *http.Request)  { s.firstOrLast(w, r, false) }

func (s *Server) firstOrLast(w http.ResponseWriter, r *http.Request, pickFirst bool) {
	ctx := r.Context()
	cexName := chi.URLParam(r, "CEX")
	source := pickSourceFromReq(s.cfg, cexName, r.URL.Query())
	reqURN := chi.URLParam(r, "URN")

	if !cite.IsCTSURN(reqURN) {
		writeJSON(w, http.StatusBadRequest, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: servicePathFirstLast(pickFirst), Message: reqURN + " is not valid CTS.",
		})
		return
	}

	allURNs, allTexts, err := s.parseCTSData(ctx, source)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: servicePathFirstLast(pickFirst), Message: "No results for " + reqURN,
		})
		return
	}

	p := strings.Split(reqURN, ":")
	if len(p) < 4 {
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: servicePathFirstLast(pickFirst), Message: "No results for " + reqURN,
		})
		return
	}
	stem := strings.Join(p[:4], ":") + ":"

	wrk := buildWorkForStem(allURNs, stem)
	if len(wrk.Passages) == 0 {
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: servicePathFirstLast(pickFirst), Message: "No results for " + reqURN,
		})
		return
	}

	var pass cite.Passage
	if pickFirst {
		pass = cite.GetFirst(wrk)
	} else {
		pass = cite.GetLast(wrk)
	}

	idx, _ := cite.GetIndexByID(pass.PassageID, wrk)
	node := Node{
		URN:      []string{pass.PassageID},
		Text:     []string{textForID(pass.PassageID, allURNs, allTexts)},
		Sequence: pass.Index,
	}
	ids := idsFromWork(wrk)
	attachNeighbors(&node, ids, idx)

	writeJSON(w, http.StatusOK, NodeResponse{
		RequestUrn: []string{reqURN}, Status: "Success", Service: servicePathFirstLast(pickFirst), Nodes: []Node{node},
	})
}

func (s *Server) handlePrev(w http.ResponseWriter, r *http.Request) { s.prevNext(w, r, false) }
func (s *Server) handleNext(w http.ResponseWriter, r *http.Request) { s.prevNext(w, r, true) }

func (s *Server) prevNext(w http.ResponseWriter, r *http.Request, wantNext bool) {
	ctx := r.Context()
	cexName := chi.URLParam(r, "CEX")
	source := pickSourceFromReq(s.cfg, cexName, r.URL.Query())
	reqURN := chi.URLParam(r, "URN")
	svc := "/texts/previous"
	if wantNext {
		svc = "/texts/next"
	}

	if !cite.IsCTSURN(reqURN) {
		writeJSON(w, http.StatusBadRequest, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: reqURN + " is not valid CTS.",
		})
		return
	}
	allURNs, allTexts, err := s.parseCTSData(ctx, source)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "No results for " + reqURN,
		})
		return
	}
	wrk := cite.Work{WorkID: "file"}
	for i, u := range allURNs {
		wrk.Passages = append(wrk.Passages, cite.Passage{PassageID: u, Index: i + 1})
	}
	wrk, _ = cite.SortPassages(wrk)

	var target cite.Passage
	if wantNext {
		target = cite.GetNext(reqURN, wrk)
	} else {
		target = cite.GetPrev(reqURN, wrk)
	}

	if target.PassageID == "" {
		writeJSON(w, http.StatusOK, NodeResponse{
			RequestUrn: []string{reqURN}, Status: "Success", Service: svc, Nodes: []Node{},
		})
		return
	}

	idx, _ := cite.GetIndexByID(target.PassageID, wrk)
	node := Node{
		URN:      []string{target.PassageID},
		Text:     []string{textForID(target.PassageID, allURNs, allTexts)},
		Sequence: target.Index,
	}
	ids := idsFromWork(wrk)
	attachNeighbors(&node, ids, idx)

	writeJSON(w, http.StatusOK, NodeResponse{
		RequestUrn: []string{reqURN}, Status: "Success", Service: svc, Nodes: []Node{node},
	})
}

func (s *Server) handleURNs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cexName := chi.URLParam(r, "CEX")
	source := pickSourceFromReq(s.cfg, cexName, r.URL.Query())
	reqURN := chi.URLParam(r, "URN")
	svc := "/texts/urns"

	if !cite.IsCTSURN(reqURN) && !cite.IsRange(reqURN) {
		writeJSON(w, http.StatusBadRequest, URNResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: reqURN + " is not valid CTS.",
		})
		return
	}
	allURNs, _, err := s.parseCTSData(ctx, source)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, URNResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "No results for " + reqURN,
		})
		return
	}

	if cite.IsRange(reqURN) {
		base := strings.Split(reqURN, ":")
		ref := strings.Split(base[4], "-")
		startPrefix := strings.Join(base[:4], ":") + ":" + ref[0]
		endPrefix := strings.Join(base[:4], ":") + ":" + ref[1]

		startIdx := -1
		endIdx := -1
		for i, id := range allURNs {
			if startIdx == -1 && strings.HasPrefix(id, startPrefix) {
				startIdx = i
			}
			if strings.HasPrefix(id, endPrefix) {
				endIdx = i
			}
		}
		if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
			writeJSON(w, http.StatusOK, URNResponse{
				RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Couldn't find URN.",
			})
			return
		}
		writeJSON(w, http.StatusOK, URNResponse{
			RequestUrn: []string{reqURN}, Status: "Success", Service: svc, URN: allURNs[startIdx : endIdx+1],
		})
		return
	}

	for _, id := range allURNs {
		if id == reqURN {
			writeJSON(w, http.StatusOK, URNResponse{
				RequestUrn: []string{reqURN}, Status: "Success", Service: svc, URN: []string{id},
			})
			return
		}
	}
	var matches []string
	for _, id := range allURNs {
		if strings.HasPrefix(id, reqURN) {
			matches = append(matches, id)
		}
	}
	if len(matches) == 0 {
		writeJSON(w, http.StatusOK, URNResponse{
			RequestUrn: []string{reqURN}, Status: "Exception", Service: svc, Message: "Couldn't find URN.",
		})
		return
	}
	writeJSON(w, http.StatusOK, URNResponse{
		RequestUrn: []string{reqURN}, Status: "Success", Service: svc, URN: matches,
	})
}
