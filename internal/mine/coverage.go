package mine

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The coverage record is prose plus a measured block. The prose is written by hand — it explains why a source
// is or is not usable — and the block between these markers is rewritten by `lineup mine`, so every number in
// the file is a command's output rather than a recollection.
const (
	CoverageBegin = "<!-- mine:begin -->"
	CoverageEnd   = "<!-- mine:end -->"
)

// RenderCoverageBlock renders the measured part of docs/coverage.md.
func RenderCoverageBlock(r Report) string {
	var b strings.Builder
	b.WriteString(CoverageBegin)
	b.WriteString("\n")
	mode := "measured"
	if r.DryRun {
		mode = "dry run — measured, nothing written"
	}
	fmt.Fprintf(&b, "_%s · %s · %s_\n\n", r.Source, mode, r.StartedAt.UTC().Format(time.RFC3339))

	b.WriteString("| Town | Candidates | Rows | Filtered | Failures | Seconds |\n|---|---:|---:|---:|---:|---:|\n")
	rows := append([]TownReport(nil), r.Towns...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Slug < rows[j].Slug })
	for _, t := range rows {
		if t.Error != "" {
			fmt.Fprintf(&b, "| %s | — | — | — | — | — |\n", t.Slug)
			continue
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %.0f |\n",
			t.Slug, t.Candidates, t.Rows, t.Filtered, townFailures(t), t.Seconds)
	}
	fmt.Fprintf(&b, "| **total** | **%d** | **%d** | **%d** | **%d** | |\n",
		r.Totals.Candidates, r.Totals.Rows, r.Totals.Filtered, r.Totals.Failures)

	for _, t := range rows {
		if t.Error != "" {
			fmt.Fprintf(&b, "\n`%s` could not be read: %s\n", t.Slug, t.Error)
		}
	}
	if reasons := aggregatedFailures(rows); len(reasons) > 0 {
		b.WriteString("\nWhy pages were lost:")
		for _, pair := range reasons {
			fmt.Fprintf(&b, " %s ×%d;", pair.reason, pair.count)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\nTowns discovered: **%d**.", r.TownCount)
	if r.Note != "" {
		fmt.Fprintf(&b, " %s", r.Note)
	}
	b.WriteString("\n")
	b.WriteString(CoverageEnd)
	return b.String()
}

type reasonCount struct {
	reason string
	count  int
}

func aggregatedFailures(rows []TownReport) []reasonCount {
	totals := map[string]int{}
	for _, t := range rows {
		for reason, count := range t.Failures {
			totals[reason] += count
		}
	}
	out := make([]reasonCount, 0, len(totals))
	for reason, count := range totals {
		out = append(out, reasonCount{reason, count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].reason < out[j].reason
	})
	return out
}

// UpdateCoverageFile replaces the measured block in path, leaving the prose alone. A file without the markers is
// an error rather than a guess: appending a table to an unmarked document is how a record stops being readable.
func UpdateCoverageFile(path string, r Report) error {
	raw, err := readFileString(path)
	if err != nil {
		return err
	}
	begin := strings.Index(raw, CoverageBegin)
	end := strings.Index(raw, CoverageEnd)
	if begin < 0 || end < begin {
		return fmt.Errorf("%s: no %s / %s markers to replace", path, CoverageBegin, CoverageEnd)
	}
	updated := raw[:begin] + RenderCoverageBlock(r) + raw[end+len(CoverageEnd):]
	if updated == raw {
		return nil
	}
	return writeFileString(path, updated)
}
