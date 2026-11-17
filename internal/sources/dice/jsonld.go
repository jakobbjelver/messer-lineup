package dice

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// jsonLDBlocks pulls every application/ld+json block out of a page.
//
// A regex over the HTML is deliberate and safe here: `<script type="application/ld+json">` is a literal
// marker, the JSON inside it is not nested HTML, and the block is the publisher's own structured data
// rather than something rendered. Parsing the DOM would add a dependency for no extra information.
var jsonLDRe = regexp.MustCompile(`(?s)<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`)

func jsonLDBlocks(body []byte) ([][]byte, error) {
	matches := jsonLDRe.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("the page carries no application/ld+json")
	}
	out := make([][]byte, 0, len(matches))
	for _, match := range matches {
		block := bytes.TrimSpace(match[1])
		// Some publishers HTML-escape the JSON; JSON-LD is supposed to be raw, but a page that escaped it
		// would otherwise look like "no structured data".
		if looksEscaped(block) {
			block = []byte(unescapeHTML(string(block)))
		}
		out = append(out, block)
	}
	return out, nil
}

func looksEscaped(b []byte) bool {
	return bytes.Contains(b, []byte("&quot;")) && !bytes.Contains(b, []byte(`"`))
}

func unescapeHTML(s string) string {
	replacer := strings.NewReplacer(
		"&quot;", `"`, "&#34;", `"`, "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&#39;", "'", "&apos;", "'",
	)
	return replacer.Replace(s)
}
