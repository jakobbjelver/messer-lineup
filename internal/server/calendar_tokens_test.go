package server

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestCalendarTokenVectors holds the Go decoder to the same vectors the board's decoder reads
// (web/src/lib/calendar.test.ts). Both runtimes answer a URL a subscriber keeps for years, so the two must agree on
// what they accept and, more importantly, on what they refuse: a token carrying a field this build does not serve is a
// 400, never a feed that quietly ignores the filter.
func TestCalendarTokenVectors(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/calendar/tokens.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var file struct {
		Vectors []struct {
			Name  string          `json:"name"`
			Token string          `json:"token"`
			OK    bool            `json:"ok"`
			Spec  json.RawMessage `json:"spec"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode vectors: %v", err)
	}
	if len(file.Vectors) == 0 {
		t.Fatal("the vector file has no vectors")
	}

	for _, vector := range file.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			got, ok := decodeCalendarSpec(vector.Token)
			if ok != vector.OK {
				t.Fatalf("accepted = %v, want %v", ok, vector.OK)
			}
			if !vector.OK {
				return
			}
			var want calendarSpec
			if err := json.Unmarshal(vector.Spec, &want); err != nil {
				t.Fatalf("the vector's spec is not a spec: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decoded %+v, want %+v", got, want)
			}
		})
	}
}
