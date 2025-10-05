package server

import (
	"context"
	"encoding/csv"
	"errors"
	"regexp"
	"strings"
)

var reLineComments = regexp.MustCompile(`(?m)[\r\n]*^//.*$`)

func (s *Server) parseCTSData(ctx context.Context, source string) (urns, texts []string, err error) {
	data, err := s.getContent(ctx, source)
	if err != nil {
		return nil, nil, err
	}
	str := string(data)
	parts := strings.Split(str, "#!ctsdata")
	if len(parts) < 2 {
		return nil, nil, errors.New("missing #!ctsdata")
	}
	section := strings.Split(parts[1], "#!")[0]
	section = reLineComments.ReplaceAllString(section, "")

	r := csv.NewReader(strings.NewReader(section))
	r.Comma = '#'
	r.LazyQuotes = true
	r.FieldsPerRecord = 2

	for {
		line, err := r.Read()
		if err != nil {
			if errors.Is(err, csv.ErrFieldCount) {
				// keep going
			}
			if err.Error() == "EOF" || errors.Is(err, context.Canceled) {
				break
			}
			if err != nil && err.Error() == "EOF" {
				break
			}
			if err != nil {
				break
			}
		}
		if len(line) != 2 {
			if len(line) == 0 {
				break
			}
			continue
		}
		urns = append(urns, strings.TrimSpace(line[0]))
		texts = append(texts, line[1])
	}
	return urns, texts, nil
}

func (s *Server) parseCTSCatalog(ctx context.Context, source string) ([]CatalogEntry, error) {
	data, err := s.getContent(ctx, source)
	if err != nil {
		return nil, err
	}
	str := string(data)
	parts := strings.Split(str, "#!ctscatalog")
	if len(parts) < 2 {
		return nil, errors.New("missing #!ctscatalog")
	}
	section := strings.Split(parts[1], "#!")[0]
	section = reLineComments.ReplaceAllString(section, "")

	r := csv.NewReader(strings.NewReader(section))
	r.Comma = '#'
	r.LazyQuotes = true

	var out []CatalogEntry
	row := 0
	for {
		fields, err := r.Read()
		if err != nil {
			break
		}
		row++
		if row == 1 && len(fields) > 0 && strings.EqualFold(strings.TrimSpace(fields[0]), "urn") {
			continue
		}
		if len(fields) < 4 {
			continue
		}
		var entry CatalogEntry
		entry.URN = strings.TrimSpace(fields[0])
		entry.CitationScheme = strings.TrimSpace(fields[1])
		entry.GroupName = strings.TrimSpace(fields[2])
		entry.WorkTitle = strings.TrimSpace(fields[3])
		if len(fields) > 4 {
			entry.VersionLabel = strings.TrimSpace(fields[4])
		}
		if len(fields) > 5 {
			entry.ExemplarLabel = strings.TrimSpace(fields[5])
		}
		if len(fields) > 6 {
			entry.Online = strings.EqualFold(strings.TrimSpace(fields[6]), "true")
		}
		out = append(out, entry)
	}
	return out, nil
}
