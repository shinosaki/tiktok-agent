package status

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"tiktok-agent/internal/live"
)

func TestSetLivesReplaces(t *testing.T) {
	s := New()
	s.SetLives([]live.Live{{RoomID: "1", Username: "a"}, {RoomID: "2", Username: "b"}})
	if got := len(s.Lives()); got != 2 {
		t.Fatalf("expected 2 lives, got %d", got)
	}
	s.SetLives([]live.Live{{RoomID: "2", Username: "b"}})
	lives := s.Lives()
	if len(lives) != 1 || lives[0].RoomID != "2" {
		t.Fatalf("expected only room 2, got %v", lives)
	}
}

func TestSetLivesPreservesDetectedAt(t *testing.T) {
	s := New()
	s.SetLives([]live.Live{{RoomID: "1", Username: "a"}})
	first := s.Lives()[0].DetectedAt
	time.Sleep(10 * time.Millisecond)
	s.SetLives([]live.Live{{RoomID: "1", Username: "a"}})
	second := s.Lives()[0].DetectedAt
	if !first.Equal(second) {
		t.Fatalf("DetectedAt should be preserved: %v != %v", first, second)
	}
}

func TestCommandLifecycle(t *testing.T) {
	s := New()
	lv := live.Live{RoomID: "1", Username: "alice"}
	c := s.CommandStarted(lv, "echo hi")
	if c.State() != StateRunning {
		t.Fatalf("expected running, got %s", c.State())
	}
	s.CommandOutput(c, false, "hello")
	s.CommandOutput(c, true, "warn")
	s.CommandFinished(c, nil)
	if c.State() != StateDone {
		t.Fatalf("expected done, got %s", c.State())
	}
	got := s.Commands()
	if len(got) != 1 {
		t.Fatalf("expected 1 command, got %d", len(got))
	}
	if out := strings.Join(got[0].Stdout.Snapshot(), ""); out != "hello" {
		t.Fatalf("unexpected stdout: %q", out)
	}
	if out := strings.Join(got[0].Stderr.Snapshot(), ""); out != "warn" {
		t.Fatalf("unexpected stderr: %q", out)
	}
}

func TestCommandFailed(t *testing.T) {
	s := New()
	c := s.CommandStarted(live.Live{RoomID: "1"}, "bad")
	s.CommandFinished(c, io.EOF)
	if c.State() != StateFailed {
		t.Fatalf("expected failed, got %s", c.State())
	}
	if c.ExitErr == "" {
		t.Fatal("expected exit error")
	}
}

func TestCommandsNewestFirst(t *testing.T) {
	s := New()
	lv := live.Live{RoomID: "1"}
	s.CommandStarted(lv, "one")
	s.CommandStarted(lv, "two")
	got := s.Commands()
	if len(got) != 2 || got[0].CommandLine != "two" {
		t.Fatalf("expected newest first, got %v", got)
	}
}

func TestLineBufferBounds(t *testing.T) {
	b := NewLineBuffer(3)
	for i := 0; i < 10; i++ {
		b.Add(fmt.Sprintf("line%d", i))
	}
	lines := b.Snapshot()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "line7" || lines[2] != "line9" {
		t.Fatalf("unexpected lines: %v", lines)
	}
	if b.Dropped() != 7 {
		t.Fatalf("expected 7 dropped, got %d", b.Dropped())
	}
}

func TestLogsBounded(t *testing.T) {
	s := New()
	for i := 0; i < 1000; i++ {
		s.AddLog("msg")
	}
	if got := len(s.Logs()); got != defaultLogLines {
		t.Fatalf("expected %d logs, got %d", defaultLogLines, got)
	}
	if got := len(s.LogsLast(5)); got != 5 {
		t.Fatalf("expected 5 logs, got %d", got)
	}
}

func TestHandlerRecords(t *testing.T) {
	s := New()
	logger := slog.New(NewHandler(s, nil))
	logger.Info("hello", "k", "v")
	lines := s.Logs()
	if len(lines) != 1 || !strings.Contains(lines[0], "hello") {
		t.Fatalf("unexpected logs: %v", lines)
	}
}

func TestHandlerWithGroup(t *testing.T) {
	s := New()
	logger := slog.New(NewHandler(s, nil)).WithGroup("g").With("k", "v")
	logger.Info("msg")
	lines := s.Logs()
	if len(lines) != 1 || !strings.Contains(lines[0], "g.k=v") {
		t.Fatalf("unexpected logs: %v", lines)
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := New()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			lv := live.Live{RoomID: fmt.Sprint(i)}
			c := s.CommandStarted(lv, "cmd")
			s.CommandOutput(c, false, "out")
			s.CommandFinished(c, nil)
			s.SetLives([]live.Live{lv})
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
			s.Lives()
			s.Commands()
			s.Logs()
			s.AddLog("log")
			time.Sleep(time.Millisecond)
		}
	}
}
