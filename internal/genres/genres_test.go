package genres

import (
	"reflect"
	"testing"
)

// The three vectors are measured, not invented: these are Last.fm's real top tags on 2026-11-22, with their counts.
func TestBucketMeasuredArtists(t *testing.T) {
	cases := []struct {
		artist string
		tags   []Tag
		want   []string
	}{
		{"Opeth", []Tag{
			{"Progressive metal", 100}, {"progressive death metal", 74}, {"death metal", 31},
			{"Melodic Death Metal", 20}, {"metal", 9}, {"Progressive rock", 6}, {"swedish", 4}, {"Progressive", 1},
		}, []string{"metal", "rock"}},
		{"Robyn", []Tag{
			{"pop", 100}, {"electronic", 93}, {"swedish", 48}, {"female vocalists", 39}, {"dance", 19},
			{"electropop", 9}, {"synthpop", 3}, {"electronica", 1},
		}, []string{"pop", "electronic"}},
		{"Thåström", []Tag{
			{"swedish", 100}, {"singer-songwriter", 67}, {"rock", 43}, {"punk", 27}, {"svenskt", 7},
			{"alternative rock", 4}, {"scandinavian", 2}, {"Sweden", 1},
		}, []string{"folk", "rock", "punk"}},
	}
	for _, test := range cases {
		if got := Bucket(test.tags); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: Bucket() = %v, want %v", test.artist, got, test.want)
		}
	}
}

func TestBucketDedupesAndCapsAtThree(t *testing.T) {
	tags := []Tag{
		{"death metal", 100}, {"black metal", 90}, {"metalcore", 80}, {"grindcore", 70}, {"doom metal", 60},
		{"rock", 50},
	}
	if got := Bucket(tags); !reflect.DeepEqual(got, []string{"metal", "rock"}) {
		t.Fatalf("Bucket() = %v, want [metal rock]", got)
	}

	tags = []Tag{
		{"pop", 100}, {"rock", 90}, {"jazz", 80}, {"blues", 70},
	}
	if got := Bucket(tags); len(got) != 3 || !reflect.DeepEqual(got, []string{"pop", "rock", "jazz"}) {
		t.Fatalf("Bucket() = %v, want three in tag order", got)
	}
}

func TestStoplistCoversTheNonGenres(t *testing.T) {
	tags := []Tag{
		{"swedish", 100}, {"seen live", 90}, {"favorites", 80}, {"female vocalists", 70},
		{"eurovision", 60}, {"80s", 50},
	}
	if got := Bucket(tags); len(got) != 0 {
		t.Fatalf("Bucket() = %v, want nothing from non-genre tags", got)
	}
}

// A tag that is not in the vocabulary contributes nothing, but it must be counted: the report is how the list grows.
func TestUnmappedIsCounted(t *testing.T) {
	tags := []Tag{
		{"shoegaze", 100}, {"metal", 90}, {"swedish", 80}, {"ghost town", 70}, {"shoegaze", 60},
	}
	got := Unmapped(tags)
	if len(got) != 1 || got[0].Name != "ghost town" || got[0].Count != 1 {
		t.Fatalf("Unmapped() = %v, want one count of ghost town", got)
	}
}

func TestNormalise(t *testing.T) {
	if got := Normalise("  Singer_Songwriter  "); got != "singer songwriter" {
		t.Fatalf("Normalise() = %q", got)
	}
}

func TestKnownAndLabel(t *testing.T) {
	for _, genre := range All {
		if !Known(genre.ID) {
			t.Fatalf("%s is not known", genre.ID)
		}
		if Label(genre.ID) != genre.Label {
			t.Fatalf("Label(%s) = %q", genre.ID, Label(genre.ID))
		}
	}
	if Known("polka") || Label("polka") != "polka" {
		t.Fatal("an unknown id must be refused and echoed")
	}
}
