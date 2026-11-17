package index

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/model"
)

const dayLayout = "2006-01-02"

// allowedKeys is the published row, exactly. `g` is the only optional one: a show whose source gave no
// coordinates sends no position rather than a wrong one.
var allowedKeys = map[string]bool{
	"k": true, "a": true, "v": true, "c": true, "d": true, "st": true, "u": true, "src": true, "g": true,
}

var requiredKeys = []string{"k", "a", "v", "c", "d", "st", "u", "src"}

// checkShape rejects a line before it is decoded: it is the schema check that makes publishing a
// content field impossible rather than merely discouraged.
func checkShape(text []byte) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(text, &obj); err != nil {
		return "is not a JSON object: " + err.Error()
	}
	for key := range obj {
		if !allowedKeys[key] {
			return fmt.Sprintf("carries %q, which is not one of the nine keys — a price, currency, description, image, line-up or seat map may never be published", key)
		}
	}
	for _, key := range requiredKeys {
		if _, ok := obj[key]; !ok {
			return fmt.Sprintf("is missing %q", key)
		}
	}
	if g, ok := obj["g"]; ok && string(g) == "null" {
		return "sends g:null; a row with no position omits g"
	}
	return ""
}

// checkRow validates the values of a decoded row.
func checkRow(row model.Row) string {
	if row.A == "" {
		return "has no artist name"
	}
	if row.V == "" {
		return "has no venue name"
	}
	if row.C == "" {
		return "has no town id"
	}
	if !row.St.Valid() {
		return fmt.Sprintf("has status %q, want one of o, c, x, s", row.St)
	}
	if dayOf(row.D) == "" {
		return fmt.Sprintf("has start %q, which is not RFC 3339", row.D)
	}
	if !strings.HasPrefix(row.K, "") || row.K == "" {
		return "has no key"
	}
	parts := strings.Split(row.K, "|")
	if len(parts) != 3 {
		return fmt.Sprintf("key %q is not artistKey|venueKey|YYYY-MM-DD", row.K)
	}
	for i, part := range parts[:2] {
		if !validKey(part) {
			return fmt.Sprintf("key half %q is not an identity key", parts[i])
		}
	}
	if parts[2] != dayOf(row.D) {
		return fmt.Sprintf("key %q disagrees with the row's date", row.K)
	}
	link, err := url.Parse(row.U)
	if err != nil || link.Host == "" {
		return fmt.Sprintf("link %q is not a URL", row.U)
	}
	if link.Scheme != "https" {
		return fmt.Sprintf("link %q is not https", row.U)
	}
	if len(row.Src) == 0 {
		return "names no source"
	}
	seen := make(map[string]bool, len(row.Src))
	for _, src := range row.Src {
		if src == "" {
			return "names an empty source"
		}
		if seen[src] {
			return fmt.Sprintf("names source %q twice", src)
		}
		seen[src] = true
	}
	if row.G != nil {
		if row.G.Lat < -90 || row.G.Lat > 90 || row.G.Lon < -180 || row.G.Lon > 180 {
			return fmt.Sprintf("position %v,%v is not on Earth", row.G.Lat, row.G.Lon)
		}
	}
	return ""
}

// validKey reports whether s has the shape Key produces: lower-case ASCII, digits and single dashes,
// no dash at either edge.
func validKey(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") || strings.Contains(s, "--") {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

// dayOf returns the date of an RFC 3339 timestamp in the venue's own offset, or "" when it does not
// parse. The date half of a key is a local date, which is why the window is compared as dates.
func dayOf(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return ""
	}
	return t.Format(dayLayout)
}
