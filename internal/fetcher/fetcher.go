package fetcher

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"tiktok-agent/internal/live"
)

// Fetcher は設定された cURL コマンドを実行し、ライブ一覧を取得する。
type Fetcher struct {
	command string
}

// New は指定したコマンドで Fetcher を生成する。
func New(command string) *Fetcher {
	return &Fetcher{command: command}
}

// FetchLives は cURL コマンドを sh -c で実行し、stdout を JSON としてパースする。
func (f *Fetcher) FetchLives(ctx context.Context) ([]live.Live, error) {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", f.command)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("fetch command failed: %w: %s", err, errBuf.String())
	}
	return live.ParseFeed(out.Bytes())
}
