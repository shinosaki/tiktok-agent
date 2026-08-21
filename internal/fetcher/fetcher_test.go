package fetcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchLives(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "feed.json")
	data, err := os.ReadFile("../../webcast_feed.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	if err := os.WriteFile(fixture, data, 0o644); err != nil {
		t.Fatal(err)
	}

	f := New("cat " + fixture)
	lives, err := f.FetchLives(context.Background())
	if err != nil {
		t.Fatalf("FetchLives failed: %v", err)
	}
	if len(lives) != 2 {
		t.Fatalf("expected 2 lives, got %d", len(lives))
	}
}

func TestFetchLivesCommandError(t *testing.T) {
	f := New("exit 1")
	if _, err := f.FetchLives(context.Background()); err == nil {
		t.Fatal("expected error for failing command")
	}
}
