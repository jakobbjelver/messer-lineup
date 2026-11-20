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

	b.WriteString("| Town | Source | Candidates | Rows | Not music | Filtered | Failures | Seconds |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|\n")
	towns := append([]TownReport(nil), r.Towns...)
	sort.SliceStable(towns, func(i, j int) bool { return towns[i].Slug < towns[j].Slug })
	for _, town := range towns {
		sources := append([]SourceReport(nil), town.Sources...)
		sort.SliceStable(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
		for _, src := range sources {
			if src.Error != "" {
				fmt.Fprintf(&b, "| %s | %s | — | — | — | — | — | — |\n", town.Slug, src.ID)
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %d | %.0f |\n",
				town.Slug, src.ID, src.Candidates, src.Rows, src.NotMusic, src.Filtered, failures(src), src.Seconds)
		}
	}
	fmt.Fprintf(&b, "| **total** | | **%d** | **%d** | **%d** | **%d** | **%d** | |\n",
		r.Totals.Candidates, r.Totals.Rows, r.Totals.NotMusic, r.Totals.Filtered, r.Totals.Failures)

	// The policy is part of the measurement: a reader must see that the board is a music board, how much was
	// dropped to make it one, and which rooms are still unclassified.
	if r.Totals.NotMusic > 0 || r.Totals.Unclassified > 0 {
		byReason := map[string]int{}
		unclassified := map[string]int{}
		for _, town := range towns {
			for _, src := range town.Sources {
				for reason, n := range src.Why {
					byReason[reason] += n
				}
				for venue, n := range src.UnclassifiedVenues {
					unclassified[venue] += n
				}
			}
		}
		fmt.Fprintf(&b, "\nMusic policy: **%d** rows dropped", r.Totals.NotMusic)
		reasons := make([]string, 0, len(byReason))
		for reason := range byReason {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		for i, reason := range reasons {
			if i == 0 {
				b.WriteString(" (")
			} else {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s %d", reason, byReason[reason])
		}
		if len(reasons) > 0 {
			b.WriteString(")")
		}
		b.WriteString(".\n")
		if len(unclassified) > 0 {
			fmt.Fprintf(&b, "%d rows at %d unclassified room(s): %s. Published, and the next pass in `sources/venues` classifies them.\n",
				r.Totals.Unclassified, len(unclassified), venueList(unclassified, 8))
		}
	}

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
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + " " + s[i:]
	}
	return s
}

// venueList names the rooms in a counter, most rows first, so the curation list in the coverage record is
// readable rather than a map dump.
func venueList(counts map[string]int, limit int) string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > limit {
		more := len(names) - limit
		return fmt.Sprintf("%s and %d more", strings.Join(names[:limit], ", "), more)
	}
	return strings.Join(names, ", ")
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
