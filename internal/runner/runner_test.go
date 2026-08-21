package runner

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tiktok-agent/internal/config"
	"tiktok-agent/internal/live"
	"tiktok-agent/internal/status"
)

func TestExpandTemplate(t *testing.T) {
	lv := live.Live{
		RoomID:    "123",
		Username:  "alice",
		StreamURL: "http://example.com/live.flv",
		Title:     "title",
	}
	got, err := expandTemplate(`ffmpeg -i "{{.StreamURL}}" -o /tmp/{{.RoomID}}_{{.Username}}.ts`, lv)
	if err != nil {
		t.Fatalf("expandTemplate failed: %v", err)
	}
	want := `ffmpeg -i "http://example.com/live.flv" -o /tmp/123_alice.ts`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpandTemplateUnknownField(t *testing.T) {
	if _, err := expandTemplate("{{.NotExist}}", live.Live{}); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestExpandTemplateSanitize(t *testing.T) {
	lv := live.Live{Username: "user:name", Title: "たいとる すぺーす/CON"}
	got, err := expandTemplate("{{sanitize .Username}}-{{sanitize .Title}}", lv)
	if err != nil {
		t.Fatalf("expandTemplate failed: %v", err)
	}
	// ':' と '/' は除去され、Windows 予約名 CON も無害化される
	if got == "" {
		t.Fatal("sanitized output should not be empty")
	}
	if strings.ContainsAny(got, `/\:*?"<>|`) {
		t.Errorf("sanitized output contains invalid characters: %q", got)
	}
}

func TestExpandTemplateNow(t *testing.T) {
	got, err := expandTemplate(`{{now.Format "20060102_150405"}}`, live.Live{})
	if err != nil {
		t.Fatalf("expandTemplate failed: %v", err)
	}
	// yyyymmdd_hhmmss 形式
	if len(got) != 15 || got[8] != '_' {
		t.Errorf("unexpected now format: %q", got)
	}
	if _, err := time.Parse("20060102_150405", got); err != nil {
		t.Errorf("invalid timestamp %q: %v", got, err)
	}
}

func TestRunLive(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(logger, nil)
	lv := live.Live{RoomID: "1", Username: "alice", StreamURL: "http://x/1.flv"}
	r.RunLive(context.Background(), lv, []config.Command{{Command: "echo hello"}}, nil)
	r.WaitDone()
}

func TestRunLiveRecordsToStore(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := status.New()
	r := New(logger, s)
	lv := live.Live{RoomID: "1", Username: "alice", StreamURL: "http://x/1.flv"}
	r.RunLive(context.Background(), lv, []config.Command{{Command: "echo hello; echo err >&2"}}, nil)
	r.WaitDone()
	cmds := s.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command recorded, got %d", len(cmds))
	}
	if cmds[0].RoomID != "1" || cmds[0].CommandLine != "echo hello; echo err >&2" {
		t.Fatalf("unexpected command record: %+v", cmds[0])
	}
	if cmds[0].State() != status.StateDone {
		t.Fatalf("expected done state, got %s", cmds[0].State())
	}
	// 出力は pipeOutput goroutine が非同期に反映するため、反映を待つ
	waitForOutput := func(snapshot func() []string, want string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			if strings.Contains(strings.Join(snapshot(), "\n"), want) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("output %q not captured: %q", want, snapshot())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitForOutput(cmds[0].Stdout.Snapshot, "hello")
	waitForOutput(cmds[0].Stderr.Snapshot, "err")
}

func TestRunLiveRetriesWhileActiveUnconditionally(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := status.New()
	r := New(logger, s)

	counter := filepath.Join(t.TempDir(), "count")
	cmdLine := `n=$(( $(cat ` + counter + ` 2>/dev/null || echo 0) + 1 )); echo $n > ` + counter + `; [ "$n" -ge 2 ]`
	cmd := config.Command{
		Command: cmdLine,
		Retry: &config.Retry{
			Enabled:    true,
			MaxRetries: 2,
			Interval: config.Interval{
				Min: config.Duration{Duration: 10 * time.Millisecond},
				Max: config.Duration{Duration: 10 * time.Millisecond},
			},
		},
	}
	lv := live.Live{RoomID: "1", Username: "alice"}
	lookup := func(roomID string) (live.Live, int, bool) { return lv, 1, true }

	r.RunLive(context.Background(), lv, []config.Command{cmd}, lookup)
	r.WaitDone()

	cmds := s.Commands()
	// アクティブな間は終了コードに関係なく max_retries+1 回まで実行する
	if len(cmds) != 3 {
		t.Fatalf("expected 3 attempts (1 + 2 retries), got %d", len(cmds))
	}
	// Commands() は新しい順: 3回目 done, 2回目 done, 1回目 failed
	if cmds[0].State() != status.StateDone || cmds[1].State() != status.StateDone || cmds[2].State() != status.StateFailed {
		t.Fatalf("unexpected states: %v, %v, %v", cmds[0].State(), cmds[1].State(), cmds[2].State())
	}
}

func TestRunLiveRetryStopsWhenLiveEnded(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := status.New()
	r := New(logger, s)

	cmd := config.Command{
		Command: "exit 1",
		Retry: &config.Retry{
			Enabled:    true,
			MaxRetries: 5,
			Interval: config.Interval{
				Min: config.Duration{Duration: time.Millisecond},
				Max: config.Duration{Duration: time.Millisecond},
			},
		},
	}
	lv := live.Live{RoomID: "1", Username: "alice"}
	// ライブがもうアクティブでない（ok=false）ためリトライしない
	lookup := func(roomID string) (live.Live, int, bool) { return lv, 1, false }

	r.RunLive(context.Background(), lv, []config.Command{cmd}, lookup)
	r.WaitDone()

	cmds := s.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 attempt only, got %d", len(cmds))
	}
}

func TestRunCommandRetryReappliesTemplate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(logger, nil)
	lv := live.Live{RoomID: "1", Username: "alice", StreamURL: "http://x/1.flv"}
	logFile := filepath.Join(t.TempDir(), "out.log")
	// ナノ秒精度のタイムスタンプを出力 → 各リトライでテンプレートが再適用され異なる値になることを検証
	cmd := retryCommand(`echo {{now.Format "20060102150405.000000000"}} >> `+logFile, 2)
	lookup := func(roomID string) (live.Live, int, bool) { return lv, 1, true }

	r.RunLive(context.Background(), lv, []config.Command{cmd}, lookup)
	r.WaitDone()

	lines, err := readLines(logFile)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	seen := make(map[string]bool)
	for _, l := range lines {
		if seen[l] {
			t.Fatalf("template was not re-applied, duplicate timestamp %q", l)
		}
		seen[l] = true
	}
}

func TestRunCommandRetryStopsOnGenerationChange(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(logger, nil)
	lv := live.Live{RoomID: "1", Username: "alice", StreamURL: "http://x/1.flv"}
	logFile := filepath.Join(t.TempDir(), "out.log")
	cmd := retryCommand("echo run >> "+logFile, 10)

	// 世代取得後に世代番号が変わる → ライブが再検出されたため停止
	lookupCalls := 0
	lookup := func(roomID string) (live.Live, int, bool) {
		lookupCalls++
		if lookupCalls == 1 {
			return lv, 1, true
		}
		return lv, 2, true
	}
	r.RunLive(context.Background(), lv, []config.Command{cmd}, lookup)
	r.WaitDone()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if got := strings.Count(string(data), "run"); got != 1 {
		t.Fatalf("expected 1 execution (generation changed), got %d", got)
	}
}

func TestRunCommandRetryStopsOnContextCancel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(logger, nil)
	lv := live.Live{RoomID: "1", Username: "alice", StreamURL: "http://x/1.flv"}
	logFile := filepath.Join(t.TempDir(), "out.log")
	cmd := retryCommand("echo run >> "+logFile, 100)
	lookup := func(roomID string) (live.Live, int, bool) { return lv, 1, true }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.RunLive(ctx, lv, []config.Command{cmd}, lookup)
		r.WaitDone()
		close(done)
	}()

	// バックオフ待機中にキャンセルする
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("retry loop did not stop after context cancel")
	}
}

func TestRunCommandWithoutRetryRunsOnce(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(logger, nil)
	lv := live.Live{RoomID: "1", Username: "alice", StreamURL: "http://x/1.flv"}
	logFile := filepath.Join(t.TempDir(), "out.log")
	// retry 無効（nil）なら lookup がアクティブを返し続けても 1 回だけ実行
	cmd := config.Command{Command: "echo run >> " + logFile}
	lookup := func(roomID string) (live.Live, int, bool) { return lv, 1, true }

	r.RunLive(context.Background(), lv, []config.Command{cmd}, lookup)
	r.WaitDone()

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if got := strings.Count(string(data), "run"); got != 1 {
		t.Fatalf("expected 1 execution without retry, got %d", got)
	}
}

// retryCommand はテスト用にリトライ有効なコマンド設定を生成する。
func retryCommand(command string, maxRetries int) config.Command {
	return config.Command{
		Command: command,
		Retry: &config.Retry{
			Enabled:    true,
			MaxRetries: maxRetries,
			Interval: config.Interval{
				Min: config.Duration{Duration: time.Millisecond},
				Max: config.Duration{Duration: time.Millisecond},
			},
		},
	}
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}
