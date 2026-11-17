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
	fmt.Fprintf(&b, "_%s · %s · %s_\n\n", strings.Join(r.Sources, ", "), mode, r.StartedAt.UTC().Format(time.RFC3339))

	b.WriteString("| Town | Source | Candidates | Rows | Filtered | Failures | Seconds |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|\n")
	towns := append([]TownReport(nil), r.Towns...)
	sort.SliceStable(towns, func(i, j int) bool { return towns[i].Slug < towns[j].Slug })
	for _, town := range towns {
		sources := append([]SourceReport(nil), town.Sources...)
		sort.SliceStable(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
		for _, src := range sources {
			if src.Error != "" {
				fmt.Fprintf(&b, "| %s | %s | — | — | — | — | — |\n", town.Slug, src.ID)
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %.0f |\n",
				town.Slug, src.ID, src.Candidates, src.Rows, src.Filtered, failures(src), src.Seconds)
		}
	}
	fmt.Fprintf(&b, "| **total** | | **%d** | **%d** | **%d** | **%d** | |\n",
		r.Totals.Candidates, r.Totals.Rows, r.Totals.Filtered, r.Totals.Failures)

	if r.Merge.Raw > 0 {
		fmt.Fprintf(&b, "\nMerged: **%d** rows → **%d** shows, duplicate rate **%.1f %%**. Towns discovered: **%d**.\n",
			r.Merge.Raw, r.Merge.Out, 100*r.Merge.DuplicateRate, r.TownCount)
	} else {
		fmt.Fprintf(&b, "\nTowns discovered: **%d**.\n", r.TownCount)
	}
	if r.Build != nil {
		fmt.Fprintf(&b, "Index: **%d** shard(s), %d rows published, %d outside the window, %d refused, %s bytes.\n",
			r.Build.Shards, r.Build.Published, r.Build.Outside, r.Build.Refused, thousands(r.Build.Bytes))
	}

	for _, town := range towns {
		for _, src := range town.Sources {
			if src.Error != "" {
				fmt.Fprintf(&b, "\n`%s` on `%s` could not be read: %s\n", town.Slug, src.ID, src.Error)
			}
			if src.Warning != "" {
				fmt.Fprintf(&b, "\n`%s` on `%s`: **%s**\n", town.Slug, src.ID, src.Warning)
			}
		}
	}
	if reasons := aggregatedFailures(towns); len(reasons) > 0 {
		b.WriteString("\nWhy pages were lost:")
		for _, pair := range reasons {
			fmt.Fprintf(&b, " %s ×%d;", pair.reason, pair.count)
		}
		b.WriteString("\n")
	}
	if r.Note != "" {
		fmt.Fprintf(&b, "\n_%s_\n", r.Note)
	}
	b.WriteString(CoverageEnd)
	return b.String()
}

type reasonCount struct {
	reason string
	count  int
}

func aggregatedFailures(towns []TownReport) []reasonCount {
	totals := map[string]int{}
	for _, town := range towns {
		for _, src := range town.Sources {
			for reason, count := range src.Failures {
				totals[reason] += count
			}
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

// thousands renders a byte count so a table stays readable without a unit library.
func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	return string(out)
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
