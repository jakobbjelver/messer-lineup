package model

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// vector is one case in testdata/keys.json. The file is shared with the TypeScript twin and is the
// contract: it is never regenerated and never edited to make an implementation pass.
type vector struct {
	In    string `json:"in"`
	Key   string `json:"key"`
	Loose string `json:"loose"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	raw, err := os.ReadFile("testdata/keys.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var cases []vector
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("vector file is empty")
	}
	return cases
}

func TestKeyVectors(t *testing.T) {
	cases := loadVectors(t)
	if len(cases) != 30 {
		t.Fatalf("expected the 30 canonical vectors, found %d", len(cases))
	}
	for _, c := range cases {
		if got := Key(c.In); got != c.Key {
			t.Errorf("Key(%q) = %q, vector says %q", c.In, got, c.Key)
		}
	}
}

func TestLooseVectors(t *testing.T) {
	for _, c := range loadVectors(t) {
		if got := Loose(c.Key); got != c.Loose {
			t.Errorf("Loose(%q) = %q, vector says %q", c.Key, got, c.Loose)
		}
	}
}

// TestKeyRules pins the individual steps, so a failure says which rule broke instead of only which
// vector disagreed.
func TestKeyRules(t *testing.T) {
	cases := map[string]string{
		"Björk":                         "bjork", // NFKD + drop marks
		"Sigur Rós":                     "sigur-ros",
		"MØ":                            "mo",      // ø does not decompose
		"Straße":                        "strasse", // ß does not decompose
		"Øresund Space Collective":      "oresund-space-collective",
		"Simon & Garfunkel":             "simon-and-garfunkel",
		"don't":                         "dont", // apostrophes are deleted, not replaced
		`"Weird Al"`:                    "weird-al",
		"Ólafur Arnalds feat. JFDR":     "olafur-arnalds",
		"Machine Gun Fellatio ft. Suzy": "machine-gun-fellatio",
		"A featuring B":                 "a",
		"Bon Iver (2)":                  "bon-iver-2",
		"The 1975":                      "the-1975", // the leading article is part of the key
		"AC/DC":                         "ac-dc",
		"   ":                           "x-" + hashPrefix("   "),
		"":                              "x-" + hashPrefix(""),
	}
	for in, want := range cases {
		if got := Key(in); got != want {
			t.Fatalf("Key(%q) = %q, want %q", in, got, want)
		}
	}
}

// hashPrefix mirrors Key's fallback so the test states the rule instead of restating an implementation.
func hashPrefix(name string) string { return strings.TrimPrefix(Key(name), "x-") }

func TestLooseRules(t *testing.T) {
	cases := map[string]string{
		"the-cure":           "cure",
		"the-the":            "the-the",  // the remainder is "the", which is not an article to drop
		"the-1975":           "the-1975", // the remainder starts with a digit
		"the-a":              "the-a",    // one character left is not enough to be a name
		"national":           "national",
		"takida-topic":       "takida",
		"sigur-ros-vevo":     "sigur-ros",
		"kvelertak-official": "kvelertak",
		"moderat-live":       "moderat-live",
		"bon-iver-2":         "bon-iver",
		"blink-182":          "blink-182", // three digits are part of the name
		"c418":               "c418",
		"x-ebd798351b":       "x-ebd798351b",
	}
	for in, want := range cases {
		if got := Loose(in); got != want {
			t.Fatalf("Loose(%q) = %q, want %q", in, got, want)
		}
	}
}

// FuzzKey holds the invariants the writer and the TypeScript reader both rely on.
func FuzzKey(f *testing.F) {
	for _, c := range loadVectorsF(f) {
		f.Add(c.In)
	}
	f.Fuzz(func(t *testing.T, name string) {
		k := Key(name)
		if k == "" {
			t.Fatalf("Key(%q) is empty", name)
		}
		for _, r := range k {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				t.Fatalf("Key(%q) = %q contains %q", name, k, r)
			}
		}
		if strings.HasPrefix(k, "-") || strings.HasSuffix(k, "-") {
			t.Fatalf("Key(%q) = %q has an edge dash", name, k)
		}
		if strings.Contains(k, "--") {
			t.Fatalf("Key(%q) = %q has an empty segment", name, k)
		}
		if again := Key(k); again != k {
			t.Fatalf("Key is not idempotent: Key(%q) = %q, Key again = %q", name, k, again)
		}
		if loose := Loose(k); loose == "" {
			t.Fatalf("Loose(%q) is empty", k)
		}
	})
}

func loadVectorsF(f *testing.F) []vector {
	f.Helper()
	raw, err := os.ReadFile("testdata/keys.json")
	if err != nil {
		f.Fatalf("read vectors: %v", err)
	}
	var cases []vector
	if err := json.Unmarshal(raw, &cases); err != nil {
		f.Fatalf("parse vectors: %v", err)
	}
	return cases
}
