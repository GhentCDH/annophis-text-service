package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"
)

// txtCache holds the parsed plain-text index. It is rebuilt on-the-fly whenever
// the set of files or their modification times change (see fingerprint).
type txtCache struct {
	mu      sync.RWMutex
	fp      string // fingerprint of files: path|mtime|size, sorted
	urns    []string
	texts   []string
	catalog []CatalogEntry
}

type txtFile struct {
	namespace string // subdirectory name → CTS namespace
	path      string
	base      string // filename without extension → work id (before ".txtparsed")
}

// scanTxtFiles walks <root>/<namespace>/*.txt (one subdirectory level) and
// returns a change fingerprint plus the discovered files, sorted by path.
func scanTxtFiles(root string) (string, []txtFile) {
	subs, err := os.ReadDir(root)
	if err != nil {
		return "", nil
	}
	var files []txtFile
	var fpParts []string
	for _, sub := range subs {
		if !sub.IsDir() {
			continue // require a namespace subdirectory
		}
		ns := sub.Name()
		subPath := filepath.Join(root, ns)
		ents, err := os.ReadDir(subPath)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".txt") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			full := filepath.Join(subPath, e.Name())
			files = append(files, txtFile{
				namespace: ns,
				path:      full,
				base:      strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
			})
			fpParts = append(fpParts, fmt.Sprintf("%s|%d|%d", full, info.ModTime().UnixNano(), info.Size()))
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	sort.Strings(fpParts)
	return strings.Join(fpParts, ";"), files
}

// parseTxtFiles reads each file and turns every non-empty line into a citable
// node with a consecutive, 1-based line URN, plus one synthetic catalog entry
// per file.
func parseTxtFiles(files []txtFile) (urns, texts []string, catalog []CatalogEntry) {
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		stem := fmt.Sprintf("urn:cts:%s:%s.txtparsed:", f.namespace, f.base)
		n := 0
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimRight(raw, "\r") // handle CRLF
			if strings.TrimSpace(line) == "" {
				continue // skip empty lines; surviving lines are numbered consecutively
			}
			n++
			urns = append(urns, fmt.Sprintf("%s%d", stem, n))
			texts = append(texts, norm.NFC.String(line))
		}
		if n > 0 {
			catalog = append(catalog, CatalogEntry{
				URN:            stem,
				CitationScheme: "line",
				GroupName:      f.namespace,
				WorkTitle:      f.base,
				Online:         true,
			})
		}
	}
	return urns, texts, catalog
}

// txtIndex returns the current plain-text index, rebuilding it only when files
// have changed. Returns nils when txt_data is unset or unreadable.
func (s *Server) txtIndex() (urns, texts []string, catalog []CatalogEntry) {
	dir := strings.TrimSpace(s.cfg.TxtData)
	if dir == "" {
		return nil, nil, nil
	}
	fp, files := scanTxtFiles(dir)

	s.txt.mu.RLock()
	if fp == s.txt.fp {
		u, t, c := s.txt.urns, s.txt.texts, s.txt.catalog
		s.txt.mu.RUnlock()
		return u, t, c
	}
	s.txt.mu.RUnlock()

	u, t, c := parseTxtFiles(files)
	s.txt.mu.Lock()
	s.txt.fp, s.txt.urns, s.txt.texts, s.txt.catalog = fp, u, t, c
	s.txt.mu.Unlock()
	return u, t, c
}

// loadCorpus returns the queryable corpus: the CEX source merged with the
// plain-text index. When txt_data is unset it is exactly parseCTSData (CEX
// behavior unchanged). When set, the CEX fetch is best-effort — plain-text works
// still resolve if the CEX source is unavailable.
func (s *Server) loadCorpus(ctx context.Context, source string) ([]string, []string, error) {
	if strings.TrimSpace(s.cfg.TxtData) == "" {
		return s.parseCTSData(ctx, source)
	}
	var cu, ct []string
	if source != "" {
		cu, ct, _ = s.parseCTSData(ctx, source) // best-effort
	}
	tu, tt, _ := s.txtIndex()
	urns := append(append([]string(nil), cu...), tu...)
	texts := append(append([]string(nil), ct...), tt...)
	if len(urns) == 0 {
		return nil, nil, errors.New("no data from CEX source or txt_data")
	}
	return urns, texts, nil
}

// loadCatalog returns catalog entries from the CEX source merged with synthetic
// entries for plain-text works. Same best-effort semantics as loadCorpus.
func (s *Server) loadCatalog(ctx context.Context, source string) ([]CatalogEntry, error) {
	if strings.TrimSpace(s.cfg.TxtData) == "" {
		return s.parseCTSCatalog(ctx, source)
	}
	var ce []CatalogEntry
	if source != "" {
		ce, _ = s.parseCTSCatalog(ctx, source) // best-effort
	}
	_, _, tc := s.txtIndex()
	return append(append([]CatalogEntry(nil), ce...), tc...), nil
}
