package dice

import "github.com/jakobbjelver/messer-lineup/internal/sources/ldjson"

// jsonLDBlocks pulls every application/ld+json block out of a page. The reader lives in internal/sources/ldjson so
// every adapter shares one idea of what a block is — including that the type attribute may be single-quoted, which
// Nortic does and this page did not.
func jsonLDBlocks(body []byte) ([][]byte, error) { return ldjson.Blocks(body) }
