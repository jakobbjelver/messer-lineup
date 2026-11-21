// Package ldjson reads schema.org JSON-LD out of a page.
//
// Two adapters now depend on the same three facts: a `<script type="application/ld+json">` block is a literal
// marker in the HTML, the block may be a single object or a list (and may nest), and its `@type` is sometimes a
// string and sometimes a list. This package is that knowledge in one place, so a third adapter does not
// re-derive it — and so a fix to the escaping rule reaches every source.
package ldjson

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// blockRe matches the script marker. A regex over the HTML is deliberate: the marker is literal, the JSON inside
// is not nested HTML, and the block is the publisher's own data rather than something rendered. The quotes around
// the type are allowed either way — Nortic writes `type='application/ld+json'`, which a double-quote-only pattern
// reads as "no structured data".
var blockRe = regexp.MustCompile(`(?s)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)

// Blocks returns every JSON-LD block on the page, unescaped when the publisher HTML-escaped it.
func Blocks(body []byte) ([][]byte, error) {
	matches := blockRe.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("the page carries no application/ld+json")
	}
	out := make([][]byte, 0, len(matches))
	for _, match := range matches {
		block := bytes.TrimSpace(match[1])
		// JSON-LD is supposed to be raw, but a page that escaped it would otherwise look like "no structured data".
		if looksEscaped(block) {
			block = []byte(unescapeHTML(string(block)))
		}
		out = append(out, block)
	}
	return out, nil
}

func looksEscaped(b []byte) bool {
	return bytes.Contains(b, []byte("&quot;")) && !bytes.Contains(b, []byte(`\"`))
}

func unescapeHTML(s string) string {
	replacer := strings.NewReplacer(
		"&quot;", `"`, "&#34;", `"`, "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&#39;", "'", "&apos;", "'",
	)
	return replacer.Replace(s)
}

// Walk calls fn for every object in a decoded JSON document, depth-first, through lists and nested objects.
//
// A schema.org block is not a schema: a publisher may wrap the event in an `@graph`, put it in a list, or nest a
// `location` that is itself a list of places. Walking by shape is what makes the reader tolerant of all three.
func Walk(value any, fn func(map[string]any)) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			Walk(item, fn)
		}
	case map[string]any:
		fn(typed)
		for _, item := range typed {
			Walk(item, fn)
		}
	}
}

// Type returns the object's `@type` as one string: a list becomes its first entry, which is the convention
// schema.org publishers use for `["Event","MusicEvent"]`.
func Type(object map[string]any) string {
	switch typed := object["@type"].(type) {
	case string:
		return typed
	case []any:
		for _, item := range typed {
			if name, ok := item.(string); ok && name != "" {
				return name
			}
		}
	}
	return ""
}

// HasType reports whether any of the object's types contains the wanted word (case-insensitive). A page that says
// `MusicEvent` must match "Event"; a page that says `EventSeries` also should.
func HasType(object map[string]any, wanted string) bool {
	wanted = strings.ToLower(wanted)
	check := func(name string) bool { return strings.Contains(strings.ToLower(name), wanted) }
	switch typed := object["@type"].(type) {
	case string:
		return check(typed)
	case []any:
		for _, item := range typed {
			if name, ok := item.(string); ok && check(name) {
				return true
			}
		}
	}
	return false
}

// String reads a string field, tolerating a number (a price, an id) and HTML entities in the text.
func String(object map[string]any, key string) string {
	switch typed := object[key].(type) {
	case string:
		return html.UnescapeString(strings.TrimSpace(typed))
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ""
}

// Object returns a nested object, or the first object of a list. This is the shape `location` and `offers` take.
func Object(object map[string]any, key string) map[string]any {
	switch typed := object[key].(type) {
	case map[string]any:
		return typed
	case []any:
		for _, item := range typed {
			if nested, ok := item.(map[string]any); ok {
				return nested
			}
		}
	}
	return nil
}
