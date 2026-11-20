package venues

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryLoadsEveryMarketAndKeysAVenueLikeARow(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "se.json", `{"format":"lineup-venues/1","market":"se","venues":[
		{"name":"Debaser Strand","class":"music"},
		{"name":"Orionteatern.","class":"other"}]}`)
	write(t, dir, "dk.json", `{"format":"lineup-venues/1","market":"dk","venues":[
		{"name":"Pumpehuset","class":"music"}]}`)

	r, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Len() != 3 {
		t.Fatalf("len = %d, want 3", r.Len())
	}
	// The trailing dot is punctuation, not identity: the row keys the room the same way.
	if c, ok := r.Class("orionteatern"); !ok || c != Other {
		t.Fatalf("orionteatern = %q,%v", c, ok)
	}
	music, other := r.Counts()
	if music != 2 || other != 1 {
		t.Fatalf("counts = %d music, %d other", music, other)
	}
}

func TestRegistryRefusesADuplicateRoomAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "se.json", `{"format":"lineup-venues/1","market":"se","venues":[{"name":"Nalen","class":"music"}]}`)
	write(t, dir, "se2.json", `{"format":"lineup-venues/1","market":"se","venues":[{"name":"nalen","class":"other"}]}`)
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("a repeated room was accepted")
	}
}

func TestRegistryRefusesAClassItCannotApply(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "se.json", `{"format":"lineup-venues/1","market":"se","venues":[{"name":"Nalen","class":"maybe"}]}`)
	if _, err := LoadDir(dir); err == nil {
		t.Fatal("an unknown class was accepted")
	}
}

func TestRegistryRefusesAnEmptyDirectory(t *testing.T) {
	if _, err := LoadDir(t.TempDir()); err == nil {
		t.Fatal("an empty registry directory was accepted")
	}
}

func TestDecide(t *testing.T) {
	r := &Registry{byKey: map[string]Class{
		"debaser-strand": Music,
		"orionteatern":   Other,
	}, name: map[string]string{}}

	cases := []struct {
		venue, title string
		keep         bool
		reason       string
	}{
		{"orionteatern", "Fairy Queen", false, "venue"},
		{"debaser-strand", "AFRO RAVE", true, ""},
		{"debaser-strand", "Julbord - eftermiddag (afternoon) 14:00", false, "form:julbord"},
		{"debaser-strand", "BODYSLAM Wrestling - WrestleWonderland", false, "form:wrestling"},
		{"debaser-strand", "Luciafrukost", false, "form:frukost"},
		// An unclassified room is kept, and says so: a new market is measurable before it is curated.
		{"vega", "Viagra Boys", true, "unclassified"},
		{"vega", "Julbord", false, "form:julbord"},
	}
	for _, tc := range cases {
		got := r.Decide(tc.venue, tc.title)
		if got.Keep != tc.keep || got.Reason != tc.reason {
			t.Errorf("Decide(%q, %q) = %v/%q, want %v/%q", tc.venue, tc.title, got.Keep, got.Reason, tc.keep, tc.reason)
		}
	}
}

// The two false-positive classes that made the first rule wrong, pinned so they cannot come back.
func TestNonMusicFormDoesNotTouchAGig(t *testing.T) {
	kept := []string{
		"Billy Opel - Södra Teatern",           // the room is appended to the title
		"NATIONALTEATERNS ROCKORKESTER",        // a band with "teater" inside its name
		"Joel Alme - Södra Teatern",            // the same shape, another artist
		"Congolese Super Night",                // "supe" is inside "Super"
		"Elton John meets Freddie Mercury",     // a tribute night, no music word anywhere
		"Jazzklubb Hjorthagen - JULAVSLUTNING", // jazz club, the word "jul" is not "julbord"
		"Travis Scott",                         // "trav" is inside a name
		"Kasino - Södra Teatern",               // "kasino" is a band, not a casino night
	}
	for _, title := range kept {
		if token := NonMusicForm(title); token != "" {
			t.Errorf("NonMusicForm(%q) = %q, want no token", title, token)
		}
	}
	dropped := map[string]string{
		"Julbord / Christmas buffet - 2026":     "julbord",
		"Humorquizet – Julspecial":              "humor",
		"Familjesöndag: baka pepparkakor":       "familjesondag",
		"Full English Standup Comedy Thursdays": "standup",
		"Orgelfest med provning":                "provning",
		// The three measured on the three-town index: an English musical, an English dinner show and a
		// community sing-along. Each sat on a music room's board beside real gigs.
		"Legally Blonde - The musical":                   "musical",
		"Christmas Dinner Show på Valand":                "dinner",
		"Allsång i Hjorthagen med Anders, Bo & Kerstin!": "allsang",
	}
	for title, want := range dropped {
		if token := NonMusicForm(title); token != want {
			t.Errorf("NonMusicForm(%q) = %q, want %q", title, token, want)
		}
	}
}
