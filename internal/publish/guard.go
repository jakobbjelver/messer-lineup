package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	lineupindex "github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// The publish guards (plan §5.8). They exist because the miner runs unattended: a parser that breaks on Tuesday
// publishes a town with a quarter of its shows, and the board goes quiet without anyone watching. A collapse is
// a reason to publish nothing and say so, not to publish less.
const (
	// MaxTotalDrop is how much of the whole index may disappear in one publish.
	MaxTotalDrop = 0.25
	// MaxTownDrop is how much one town may lose while its own sources still report ok — the signature of a
	// parser that stopped matching rather than a town that got quieter.
	MaxTownDrop = 0.60
)

// Refused is a publish the guards stopped. The caller prints the problems; nothing was uploaded.
type Refused struct {
	Problems []string
}

func (r Refused) Error() string {
	if len(r.Problems) == 0 {
		return "publish: refused"
	}
	return "publish: refused — " + r.Problems[0]
}

// LoadPrevious reads the manifest already in the bucket. A missing manifest is not an error: it is the first
// publish, and there is nothing to compare against.
func (o Options) LoadPrevious(ctx context.Context) (*model.Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.objectURL(lineupindex.ManifestFile), nil)
	if err != nil {
		return nil, err
	}
	o.sign(req, nil)
	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("publish: GET %s: %w", lineupindex.ManifestFile, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("publish: GET %s: %s", lineupindex.ManifestFile, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("publish: reading the published manifest: %w", err)
	}
	var previous model.Manifest
	if err := json.Unmarshal(body, &previous); err != nil {
		// A manifest we cannot read is not a reason to publish over it blind.
		return nil, fmt.Errorf("publish: the published manifest does not parse: %w", err)
	}
	return &previous, nil
}

// Check returns the reasons this index must not replace the published one. An empty result means publish.
//
// The plan's fourth rule — "fewer than two sources ok for the primary town" — cannot be transcribed: ADR-003
// established that no town we mine has two sources (DICE is DE/GB/US, Tickster is DK/NO/SE), so a two-source
// threshold would refuse every publish forever. The rule it was protecting is kept instead: a town that loses
// most of its rows while the source behind it still says "ok" is a parser break, not a quiet season.
func Check(previous, current *model.Manifest) []string {
	if previous == nil || previous.Towns == nil {
		return nil // nothing published yet
	}
	var problems []string

	before, after := rows(previous), rows(current)
	if before > 0 && after < int(float64(before)*(1-MaxTotalDrop)) {
		problems = append(problems, fmt.Sprintf(
			"the index would drop from %d to %d rows (%.0f %%, over the %.0f %% limit)",
			before, after, 100*(1-float64(after)/float64(before)), 100*MaxTotalDrop))
	}

	okSources := map[string]bool{}
	for _, source := range current.Sources {
		okSources[source.ID] = source.OK
	}
	now := map[string]model.Index{}
	for _, town := range current.Towns {
		now[town.Town] = town
	}
	for _, town := range previous.Towns {
		if town.Count < 20 {
			// A small town losing most of its rows is ordinary: three shows becoming one is not a break, and a
			// guard that fires on it teaches people to pass --force by habit.
			continue
		}
		entry, still := now[town.Town]
		count := 0
		if still {
			count = entry.Count
		}
		if count > int(float64(town.Count)*(1-MaxTownDrop)) {
			continue
		}
		allOK := len(town.Sources) > 0
		for _, id := range town.Sources {
			if !okSources[id] {
				allOK = false
			}
		}
		if !allOK {
			// The source reported a failure; the drop is explained, and refusing would hide it.
			continue
		}
		if !still {
			problems = append(problems, fmt.Sprintf("%s would disappear (had %d rows) while its sources report ok", town.Town, town.Count))
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s would drop from %d to %d rows while its sources report ok",
			town.Town, town.Count, entry.Count))
	}
	return problems
}

func rows(m *model.Manifest) int {
	total := 0
	for _, town := range m.Towns {
		total += town.Count
	}
	return total
}
