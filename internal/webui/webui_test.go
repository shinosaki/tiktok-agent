package webui

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"tiktok-agent/internal/live"
	"tiktok-agent/internal/status"
)

func newTestServer(t *testing.T) (*Server, *status.Store) {
	t.Helper()
	s := status.New()
	server, err := New(s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return server, s
}

func TestIndex(t *testing.T) {
	server, s := newTestServer(t)
	s.SetLives([]live.Live{{RoomID: "1", Username: "alice", Title: "hello", Nickname: "アリス"}})
	c := s.CommandStarted(live.Live{RoomID: "1", Username: "alice"}, "echo hi")
	s.CommandOutput(c, false, "hello out")
	s.CommandOutput(c, true, "hello err")
	s.CommandFinished(c, nil)
	s.AddLog("log line")

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"alice", "アリス", "hello", "echo hi", "hello out", "hello err", "log line", "done"} {
		if !strings.Contains(body, want) {
			t.Fatalf("index missing %q:\n%s", want, body)
		}
	}
}

func TestIndexNotFound(t *testing.T) {
	server, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestAPILives(t *testing.T) {
	server, s := newTestServer(t)
	s.SetLives([]live.Live{{RoomID: "1", Username: "alice"}})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/lives", nil))
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var lives []status.Live
	if err := json.Unmarshal(rec.Body.Bytes(), &lives); err != nil {
		t.Fatal(err)
	}
	if len(lives) != 1 || lives[0].RoomID != "1" || lives[0].Username != "alice" {
		t.Fatalf("unexpected lives: %v", lives)
	}
}

func TestAPICommands(t *testing.T) {
	server, s := newTestServer(t)
	c := s.CommandStarted(live.Live{RoomID: "1", Username: "alice"}, "echo hi")
	s.CommandOutput(c, false, "hello")
	s.CommandFinished(c, nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/commands", nil))
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var cmds []status.Command
	if err := json.Unmarshal(rec.Body.Bytes(), &cmds); err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].RoomID != "1" || cmds[0].State() != status.StateDone {
		t.Fatalf("unexpected commands: %v", cmds)
	}
	if got := cmds[0].Stdout.Snapshot(); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("unexpected stdout: %v", got)
	}
}

func TestAPILogs(t *testing.T) {
	server, s := newTestServer(t)
	s.AddLog("log1")
	s.AddLog("log2")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/logs", nil))
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var logs []string
	if err := json.Unmarshal(rec.Body.Bytes(), &logs); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0] != "log1" || logs[1] != "log2" {
		t.Fatalf("unexpected logs: %v", logs)
	}
}
