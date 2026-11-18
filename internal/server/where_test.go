package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The towns this test drives: a Malmö-like visitor between Copenhagen (28 km) and Stockholm (500 km), both `SE`.
const (
	whereTowns = "t=copenhagen-dk@55.6761,12.5683&t=stockholm-se@59.3293,18.0686"
	malmo      = "55.6050,13.0038"
)

func whereFor(t *testing.T, handler http.Handler, query string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/where?"+query, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	handler.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

// A country is not a distance: Malmö is in SE, and Copenhagen is the nearer town. The edge's position decides, and the
// answer is private because it is about one visitor.
func TestWherePrefersTheNearerTown(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	code, body := whereFor(t, handler, whereTowns, map[string]string{
		"CF-IPCountry":   "SE",
		"CF-IPLatitude":  "55.6050",
		"CF-IPLongitude": "13.0038",
	})
	if code != http.StatusOK || body["town"] != "copenhagen-dk" || body["basis"] != "distance" {
		t.Fatalf("Malmö → %d %v", code, body)
	}
	if km, ok := body["km"].(float64); !ok || km < 15 || km > 45 {
		t.Fatalf("Malmö → Copenhagen measured %v km, want ~28", body["km"])
	}

	// A visitor in Stockholm gets Stockholm, from the same pair of towns.
	_, body = whereFor(t, handler, whereTowns, map[string]string{
		"CF-IPCountry":   "SE",
		"CF-IPLatitude":  "59.3293",
		"CF-IPLongitude": "18.0686",
	})
	if body["town"] != "stockholm-se" || body["basis"] != "distance" {
		t.Fatalf("Stockholm → %v", body)
	}
}

// Without a position the country suffix decides, and without a country the first town — and both answers are shared.
func TestWhereFallsBackToTheCountry(t *testing.T) {
	handler := newServer(t, buildIndex(t))

	_, body := whereFor(t, handler, whereTowns, map[string]string{"CF-IPCountry": "dk"})
	if body["town"] != "copenhagen-dk" || body["basis"] != "country" || body["km"] != nil {
		t.Fatalf("dk without a position → %v", body)
	}
	_, body = whereFor(t, handler, whereTowns, map[string]string{"CF-IPCountry": "JP"})
	if body["town"] != "copenhagen-dk" || body["basis"] != "fallback" {
		t.Fatalf("JP → %v", body)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/where?"+whereTowns, nil))
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=86400" {
		t.Fatalf("a country answer was cached %q, want public", got)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/where?"+whereTowns, nil)
	req.Header.Set("CF-IPLatitude", "55.6050")
	req.Header.Set("CF-IPLongitude", "13.0038")
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=86400" {
		t.Fatalf("a nearest answer was cached %q, want private", got)
	}
}

// A town whose venues never stated a position still competes on country, and an unknown pair is not believed.
func TestWhereIgnoresUnknownPositions(t *testing.T) {
	handler := newServer(t, buildIndex(t))
	code, body := whereFor(t, handler, "t=copenhagen-dk@0,0&t=stockholm-se@0,0", map[string]string{
		"CF-IPCountry":   "SE",
		"CF-IPLatitude":  "55.6050",
		"CF-IPLongitude": "13.0038",
	})
	if code != http.StatusOK || body["town"] != "stockholm-se" || body["basis"] != "country" {
		t.Fatalf("unknown positions → %d %v", code, body)
	}
	if code, _ := whereFor(t, handler, "t=", nil); code != http.StatusBadRequest {
		t.Fatalf("empty t = %d, want 400", code)
	}
	if code, _ := whereFor(t, handler, "t=@55,13", nil); code != http.StatusBadRequest {
		t.Fatalf("an entry with no id = %d, want 400", code)
	}
}
