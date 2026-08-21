package runner

import (
	"bytes"
	"context"
	"log/slog"
	"math/rand/v2"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/spf13/pathologize"

	"tiktok-agent/internal/config"
	"tiktok-agent/internal/live"
	"tiktok-agent/internal/status"
)

// templateFuncs はコマンドテンプレートで使用できる関数群。
var templateFuncs = template.FuncMap{
	// sanitize: ファイル名として安全な文字列にサニタイズする（pathologize.Clean）
	"sanitize": pathologize.Clean,
	// now: 現在時刻（time.Time）を返す。{{now.Format "20060102_150405"}} のように使う
	"now": time.Now,
}

// Runner はテンプレート展開とコマンド実行を行う。
type Runner struct {
	logger *slog.Logger
	store  *status.Store
	wg     sync.WaitGroup
}

// New は Runner を生成する。store が nil の場合はコマンド記録を行わない。
func New(logger *slog.Logger, store *status.Store) *Runner {
	return &Runner{logger: logger, store: store}
}

// RunLive はライブに対して全コマンドを実行する。各コマンドは独立した goroutine で並列実行する。
// lookup はリトライ時にライブのアクティブ状態・最新データ・世代番号の取得に使用する。
func (r *Runner) RunLive(ctx context.Context, lv live.Live, commands []config.Command,
	lookup func(roomID string) (live.Live, int, bool)) {
	for _, c := range commands {
		c := c
		r.wg.Go(func() {
			r.runCommandWithRetry(ctx, lv, c, lookup)
		})
	}
}

// WaitDone は実行中の全コマンドの終了を待つ。
func (r *Runner) WaitDone() {
	r.wg.Wait()
}

// runCommandWithRetry はコマンドを実行する。リトライ設定があれば、ライブがアクティブな間は
// 終了コードに関係なく max_retries+1 回まで実行する。
func (r *Runner) runCommandWithRetry(ctx context.Context, lv live.Live, c config.Command,
	lookup func(roomID string) (live.Live, int, bool)) {
	retry := c.Retry
	maxAttempts := 1
	if retry != nil && retry.Enabled {
		maxAttempts = retry.MaxRetries + 1
	}

	// 検出時の世代番号を記録し、ライブが消えて再検出された場合はリトライを中断する
	startGen := 0
	if lookup != nil {
		if _, g, ok := lookup(lv.RoomID); ok {
			startGen = g
		}
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// リトライ前にライブのアクティブ状態と世代を確認する
			if lookup != nil {
				latest, g, ok := lookup(lv.RoomID)
				if !ok || g != startGen {
					return
				}
				lv = latest
			}
			r.logger.Info("retrying command",
				"room_id", lv.RoomID, "attempt", attempt+1, "max_retries", maxAttempts-1)
			select {
			case <-ctx.Done():
				return
			case <-time.After(randomInterval(retry.Interval.Min.Duration, retry.Interval.Max.Duration)):
			}
		}
		// 終了コードに関係なくライブがアクティブな間は実行する
		_ = r.runCommand(ctx, lv, c.Command)
	}
}

// runCommand はコマンドを 1 回実行し、エラーがあれば返す。
func (r *Runner) runCommand(ctx context.Context, lv live.Live, tmpl string) error {
	cmdLine, err := expandTemplate(tmpl, lv)
	if err != nil {
		r.logger.Error("template expansion failed", "room_id", lv.RoomID, "error", err)
		return err
	}
	var rec *status.Command
	if r.store != nil {
		rec = r.store.CommandStarted(lv, cmdLine)
	}
	r.logger.Info("executing command",
		"room_id", lv.RoomID, "username", lv.Username, "command", cmdLine)

	// CommandContext はデフォルトで ctx キャンセル時に SIGKILL を送る。
	// SIGINT を送るよう Cancel を上書きする。
	// 子プロセス（シェル→ffmpeg 等）にも届くようプロセスグループ単位で送る。
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", cmdLine)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	}
	// SIGINT 後、プロセスが終了しない場合は WaitDelay 経過後に SIGKILL で強制終了する
	cmd.WaitDelay = 10 * time.Second
	outW := &outputWriter{r: r, lv: lv, rec: rec, isErr: false}
	errW := &outputWriter{r: r, lv: lv, rec: rec, isErr: true}
	cmd.Stdout = outW
	cmd.Stderr = errW
	if err := cmd.Start(); err != nil {
		r.finish(rec, err)
		r.logger.Error("failed to start command", "room_id", lv.RoomID, "error", err)
		return err
	}
	werr := cmd.Wait()
	outW.flush()
	errW.flush()
	r.finish(rec, werr)
	if werr != nil {
		r.logger.Error("command finished with error", "room_id", lv.RoomID, "error", werr)
	}
	return werr
}

// finish はコマンド終了をストアに記録する。
func (r *Runner) finish(rec *status.Command, err error) {
	if r.store != nil {
		r.store.CommandFinished(rec, err)
	}
}

// outputWriter はコマンドの stdout/stderr を行単位でログとストアに流す io.Writer。
// cmd.Stdout/Stderr に直接設定することで、exec がコマンド終了までデータをコピーしてくれるため、
// StdoutPipe と goroutine 読み出しを併用した場合に発生する出力の取りこぼし
// （Wait が pipe を閉じる際の未読データ破棄）がない。
type outputWriter struct {
	r     *Runner
	lv    live.Live
	rec   *status.Command
	isErr bool
	buf   []byte
}

func (w *outputWriter) Write(p []byte) (int, error) {
	data := append(w.buf, p...)
	start := 0
	for {
		idx := bytes.IndexByte(data[start:], '\n')
		if idx < 0 {
			break
		}
		w.emit(strings.TrimRight(string(data[start:start+idx]), "\r"))
		start += idx + 1
	}
	w.buf = append(w.buf[:0], data[start:]...)
	return len(p), nil
}

// flush は改行なしで残った行を出力する。
func (w *outputWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

func (w *outputWriter) emit(line string) {
	if w.r.store != nil {
		w.r.store.CommandOutput(w.rec, w.isErr, line)
	}
	level := slog.LevelInfo
	if w.isErr {
		level = slog.LevelWarn
	}
	w.r.logger.Log(context.Background(), level, line, "room_id", w.lv.RoomID)
}

// randomInterval は指定範囲からランダムなリトライ間隔を決定する。
func randomInterval(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	return min + time.Duration(rand.Int64N(int64(max-min)))
}

// expandTemplate は text/template でコマンド文字列を展開する。
func expandTemplate(tmpl string, lv live.Live) (string, error) {
	t, err := template.New("command").Funcs(templateFuncs).Parse(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, lv); err != nil {
		return "", err
	}
	return b.String(), nil
}
