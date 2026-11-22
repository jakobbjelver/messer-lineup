package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jakobbjelver/messer-lineup/internal/genres"
	"github.com/jakobbjelver/messer-lineup/internal/model"
)

// genreArtists resolves a token's genre ids to the artist keys the published artifact lists.
//
// A build that has not published genres yet answers an empty set, which makes a genre-only feed an empty calendar rather
// than the whole town — the plan's rule, and the honest one: the subscriber asked for a filter, not for everything.
func (s *server) genreArtists(manifest model.Manifest, ids []string) (map[string]bool, error) {
	out := make(map[string]bool)
	name := manifest.Files["genres"]
	if name == "" {
		return out, nil
	}
	byID, err := s.genreIndex(name)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		for _, key := range byID[id] {
			out[key] = true
		}
	}
	return out, nil
}

// genreIndex reads the artifact once per published name. The name is content-addressed, so a new nightly is a new name
// and the cache misses exactly once.
func (s *server) genreIndex(name string) (map[string][]string, error) {
	s.genreMu.Lock()
	defer s.genreMu.Unlock()
	if s.genreCache != nil && s.genreName == name {
		return s.genreCache, nil
	}
	raw, err := os.ReadFile(filepath.Join(s.opts.IndexDir, filepath.FromSlash(name)))
	if err != nil {
		return nil, fmt.Errorf("genres: %w", err)
	}
	var document struct {
		Format string              `json:"format"`
		Genres map[string][]string `json:"genres"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("genres: %s: %w", name, err)
	}
	if document.Format != genres.Format {
		return nil, fmt.Errorf("genres: %s is %q, want %q", name, document.Format, genres.Format)
	}
	if document.Genres == nil {
		document.Genres = map[string][]string{}
	}
	s.genreCache, s.genreName = document.Genres, name
	return s.genreCache, nil
}

// validGenreID is the token's grammar for one id: lowercase letters, digits and hyphens.
func validGenreID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}
