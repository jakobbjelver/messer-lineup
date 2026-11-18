package server

// Which town a visitor should land on, by country.
//
// The board is static, so it cannot know where a request came from; the edge can. The client sends the published town
// ids, and the answer is the one whose country suffix matches the request's country — every id is `<slug>-<cc>` by
// construction, so no table is needed. The Worker reads `CF-IPCountry`; this server reads the same header, which is
// what lets the contract hold both runtimes to one answer, and a header set by a test is what makes the branch
// testable without an edge.

import (
	"net/http"
	"strings"
)

func (s *server) where(w http.ResponseWriter, r *http.Request) {
	country := strings.ToLower(strings.TrimSpace(r.Header.Get("CF-IPCountry")))
	raw := strings.Split(r.URL.Query().Get("towns"), ",")
	ids := make([]string, 0, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 64 {
			continue
		}
		ids = append(ids, id)
		if len(ids) == 50 {
			break
		}
	}
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "towns is required: the published town ids")
		return
	}
	town, basis := ids[0], "fallback"
	if country != "" {
		for _, id := range ids {
			if strings.HasSuffix(id, "-"+country) {
				town, basis = id, "country"
				break
			}
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Vary", "CF-IPCountry")
	writeJSON(w, http.StatusOK, map[string]any{
		"town":    town,
		"country": orNil(country),
		"basis":   basis,
	})
}

func orNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}
