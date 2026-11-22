// Command fixture writes a small, verifiable index for the design probe.
//
// The probe in CI cannot run against the live bucket — it needs a fixed board with known rows — and it cannot use a
// committed index either, because a board is a window: dates written in 2026 read as an empty page in 2027 and the
// probe would fail for a reason that is not a regression. So the fixture is generated at run time, dated relative to
// today, and written through the same `index.Write` the miner uses (content-addressed shards, a manifest, a window).
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jakobbjelver/messer-lineup/internal/clock"
	"github.com/jakobbjelver/messer-lineup/internal/index"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

type seed struct {
	artist, venue, town, townName string
	// Days from today, hour, latitude/longitude (0,0 means the source publishes no position).
	offset, hour int
	lat, lon     float64
}

var seeds = []seed{
	{"Opeth", "Debaser Strand", "stockholm-se", "Stockholm", 2, 20, 59.3153, 18.0722},
	{"Katatonia", "Fryshuset", "stockholm-se", "Stockholm", 3, 19, 59.3031, 18.0947},
	{"Viagra Boys", "Debaser Strand", "stockholm-se", "Stockholm", 5, 21, 59.3153, 18.0722},
	{"Anna von Hausswolff", "Storkyrkan", "stockholm-se", "Stockholm", 8, 18, 59.3257, 18.0704},
	{"Efterklang", "Södra Teatern", "stockholm-se", "Stockholm", 11, 19, 59.3172, 18.0748},
	{"Thåström", "Annexet", "stockholm-se", "Stockholm", 16, 20, 59.2936, 18.0838},
	{"The Hellacopters", "Pustervik", "gothenburg-se", "Göteborg", 2, 21, 57.7013, 11.9661},
	{"Robyn", "Lisebergshallen", "gothenburg-se", "Göteborg", 6, 20, 57.6945, 11.9955},
	{"Bo Kaspers Orkester", "Nefertiti", "gothenburg-se", "Göteborg", 9, 19, 57.7065, 11.9737},
	{"Håkan Hellström", "Bananas", "gothenburg-se", "Göteborg", 13, 22, 57.7018, 11.9589},
	{"Lisa Nilsson", "Pustervik", "gothenburg-se", "Göteborg", 18, 19, 57.7013, 11.9661},
	{"Amanda Ginsburg", "Nefertiti", "gothenburg-se", "Göteborg", 24, 19, 57.7065, 11.9737},
}

func main() {
	dir := flag.String("dir", "testdata/index", "where to write the fixture index")
	flag.Parse()

	now := clock.Now()
	built := now.UTC()
	shows := make([]model.Show, 0, len(seeds))
	for index, entry := range seeds {
		start := time.Date(now.Year(), now.Month(), now.Day(), entry.hour, 0, 0, 0, time.UTC).
			AddDate(0, 0, entry.offset)
		var point *model.Geo
		if entry.lat != 0 || entry.lon != 0 {
			point = &model.Geo{Lat: entry.lat, Lon: entry.lon}
		}
		shows = append(shows, model.Show{
			Artist:    entry.artist,
			ArtistKey: model.Key(entry.artist),
			Venue:     entry.venue,
			VenueKey:  model.Key(entry.venue),
			Town:      entry.town,
			TownName:  entry.townName,
			Start:     start,
			Status:    model.OnSale,
			URL:       fmt.Sprintf("https://example.test/show/%d", index+1),
			Geo:       point,
			Sources:   []string{"fixture"},
		})
	}

	set := index.NewTownSet()
	for _, show := range shows {
		set.Add(show)
	}
	if err := os.RemoveAll(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "fixture: %v\n", err)
		os.Exit(1)
	}
	manifest, stats, err := index.Write(*dir, shows, index.WriteOptions{
		BuiltAt: built,
		Window:  model.Window{From: built.AddDate(0, 0, -1), To: built.AddDate(0, 0, 130), Days: 131},
		Sources: []model.Source{{ID: "fixture", Index: "generated", OK: true, Rows: len(shows)}},
		Towns:   set.Towns(built).Towns,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "fixture: %s · %d towns · %d rows · %d bytes\n", *dir, len(manifest.Towns), stats.Published, stats.Bytes)
}
