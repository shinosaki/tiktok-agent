package cli

import (
	"strings"
	"testing"

	"tiktok-agent/internal/live"
	"tiktok-agent/internal/status"
)

func TestRenderSections(t *testing.T) {
	s := status.New()
	s.SetLives([]live.Live{{RoomID: "1", Username: "alice", Title: "hello", ViewerCount: 5, LikeCount: 10}})
	c := s.CommandStarted(live.Live{RoomID: "1", Username: "alice"}, "echo hi")
	s.CommandOutput(c, false, "out line")
	s.CommandOutput(c, true, "err line")
	s.CommandFinished(c, nil)
	s.AddLog("log line")

	out := Render(s)
	for _, want := range []string{
		"[アクティブなライブ]", "[コマンド]", "[ログ (最新20件)]",
		"alice", "hello", "echo hi", "out line", "err line", "log line", "done",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderEmpty(t *testing.T) {
	s := status.New()
	out := Render(s)
	for _, want := range []string{"(なし)", "アクティブライブ: 0", "コマンド: 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderRunningCommand(t *testing.T) {
	s := status.New()
	s.CommandStarted(live.Live{RoomID: "1"}, "sleep 100")
	out := Render(s)
	if !strings.Contains(out, "running") {
		t.Fatalf("render should show running state:\n%s", out)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("あいうえお", 3); got != "あい…" {
		t.Fatalf("unexpected truncate result: %q", got)
	}
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("unexpected truncate result: %q", got)
	}
}
