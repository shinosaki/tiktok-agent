package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"tiktok-agent/internal/status"
)

// RefreshInterval は画面の再描画間隔。
const RefreshInterval = time.Second

// Run は ctx がキャンセルされるまで、一定間隔で状態を画面に描画する。
func Run(ctx context.Context, store *status.Store) error {
	render(store)
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			render(store)
		}
	}
}

// render は画面をクリアして状態を描画する。
func render(store *status.Store) {
	clear := "\x1b[H\x1b[2J"
	if !isTerminal(os.Stdout) {
		// 非端末では区切り行を出力して流す
		clear = "\n" + strings.Repeat("=", 60) + "\n"
	}
	fmt.Print(clear + Render(store))
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Render はストアのスナップショットから画面文字列を組み立てる（テスト可能な純関数）。
func Render(store *status.Store) string {
	lives := store.Lives()
	commands := store.Commands()
	logs := store.LogsLast(20)

	var b strings.Builder
	header := fmt.Sprintf("tiktok-agent  %s  アクティブライブ: %d  コマンド: %d",
		time.Now().Format("2006-01-02 15:04:05"), len(lives), len(commands))
	b.WriteString(header + "\n")
	b.WriteString(strings.Repeat("-", 100) + "\n")

	b.WriteString("\n[アクティブなライブ]\n")
	if len(lives) == 0 {
		b.WriteString("  (なし)\n")
	} else {
		b.WriteString(fmt.Sprintf("  %-20s %-16s %-20s %-30s %8s %10s %s\n",
			"ROOMID", "USERNAME", "NICKNAME", "TITLE", "VIEWERS", "LIKES", "DETECTED"))
		for _, lv := range lives {
			b.WriteString(fmt.Sprintf("  %-20s %-16s %-20s %-30s %8d %10d %s\n",
				truncate(lv.RoomID, 20), truncate(lv.Username, 16), truncate(lv.Nickname, 20),
				truncate(lv.Title, 30), lv.ViewerCount, lv.LikeCount,
				lv.DetectedAt.Format("15:04:05")))
		}
	}

	b.WriteString("\n[コマンド]\n")
	if len(commands) == 0 {
		b.WriteString("  (なし)\n")
	}
	for _, c := range commands {
		b.WriteString(fmt.Sprintf("  #%-4s %-12s %-20s %s\n",
			c.ID, c.State(), truncate(c.Username, 20), truncate(c.CommandLine, 80)))
		b.WriteString(fmt.Sprintf("      start: %s", c.StartedAt.Format("15:04:05")))
		if c.FinishedAt != nil {
			b.WriteString(fmt.Sprintf("  end: %s", c.FinishedAt.Format("15:04:05")))
		}
		if c.ExitErr != "" {
			b.WriteString("  exit: " + c.ExitErr)
		}
		b.WriteString("\n")
		appendTail := func(name string, buf *status.LineBuffer) {
			if buf == nil {
				return
			}
			lines := buf.Snapshot()
			if len(lines) == 0 {
				return
			}
			tail := lines
			const maxTail = 5
			if len(tail) > maxTail {
				tail = tail[len(tail)-maxTail:]
			}
			b.WriteString(fmt.Sprintf("      %s (last %d/%d):\n", name, len(tail), len(lines)))
			for _, ln := range tail {
				b.WriteString("        " + ln + "\n")
			}
		}
		appendTail("stdout", c.Stdout)
		appendTail("stderr", c.Stderr)
	}

	b.WriteString("\n[ログ (最新20件)]\n")
	if len(logs) == 0 {
		b.WriteString("  (なし)\n")
	}
	for _, ln := range logs {
		b.WriteString("  " + ln + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// truncate は文字列を最大 n 文字（表示幅ではなくルーン数）で切り詰める。
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
