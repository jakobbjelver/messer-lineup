package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func whereFor(t *testing.T, handler http.Handler, country, towns string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/where?towns="+towns, nil)
	if country != "" {
		req.Header.Set("CF-IPCountry", country)
	}
	handler.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestWherePicksTheTownsCountry(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	towns := "copenhagen-dk,stockholm-se"

	code, body := whereFor(t, handler, "SE", towns)
	if code != http.StatusOK || body["town"] != "stockholm-se" || body["basis"] != "country" {
		t.Fatalf("SE → %d %v", code, body)
	}
	code, body = whereFor(t, handler, "dk", towns)
	if body["town"] != "copenhagen-dk" || body["basis"] != "country" {
		t.Fatalf("dk → %v", body)
	}

	// A country the index does not cover, and no header at all: the first published town, and it says so.
	for _, tc := range []struct{ country, label string }{{"JP", "JP"}, {"", "no header"}} {
		code, body = whereFor(t, handler, tc.country, towns)
		if code != http.StatusOK || body["town"] != "copenhagen-dk" || body["basis"] != "fallback" {
			t.Fatalf("%s → %d %v", tc.label, code, body)
		}
	}
}

func TestWhereRefusesAnEmptyList(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	if code, _ := whereFor(t, handler, "SE", ""); code != http.StatusBadRequest {
		t.Fatalf("empty towns = %d, want 400", code)
	}
}
