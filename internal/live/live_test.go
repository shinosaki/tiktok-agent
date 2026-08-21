package live

import (
	"os"
	"testing"
)

func TestParseFeed(t *testing.T) {
	data, err := os.ReadFile("../../webcast_feed.json")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	lives, err := ParseFeed(data)
	if err != nil {
		t.Fatalf("ParseFeed failed: %v", err)
	}
	if len(lives) != 2 {
		t.Fatalf("expected 2 lives, got %d", len(lives))
	}

	lv := lives[0]
	if lv.RoomID != "12345" {
		t.Errorf("unexpected RoomID: %s", lv.RoomID)
	}
	if lv.Username != "username" {
		t.Errorf("unexpected Username: %s", lv.Username)
	}
	if lv.Nickname == "" {
		t.Error("Nickname should not be empty")
	}
	if lv.StreamURL == "" {
		t.Error("StreamURL should not be empty")
	}

	second := lives[1]
	if second.Username != "username2" {
		t.Errorf("unexpected Username: %s", second.Username)
	}
	if second.Title == "" {
		t.Error("Title should not be empty")
	}
}

func TestParseFeedEmptyData(t *testing.T) {
	lives, err := ParseFeed([]byte(`{"status_code":0,"data":[]}`))
	if err != nil {
		t.Fatalf("ParseFeed failed: %v", err)
	}
	if len(lives) != 0 {
		t.Fatalf("expected 0 lives, got %d", len(lives))
	}
}

func TestParseFeedInvalidJSON(t *testing.T) {
	if _, err := ParseFeed([]byte(`not json`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseFeedNonZeroStatus(t *testing.T) {
	if _, err := ParseFeed([]byte(`{"status_code":1,"data":[]}`)); err == nil {
		t.Fatal("expected error for non-zero status_code")
	}
}
