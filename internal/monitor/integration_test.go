package monitor

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"tiktok-agent/internal/config"
	"tiktok-agent/internal/fetcher"
	"tiktok-agent/internal/live"
)

// 実データ（webcast_feed.json）を実フェッチャーで読み、複数回ポーリングして
// 同一ライブの重複ディスパッチが起きないことを確認する統合テスト。
func TestIntegrationNoDuplicateDispatch(t *testing.T) {
	cfg := &config.Config{}
	cfg.PollingInterval.Min.Duration = 100 * time.Millisecond
	cfg.PollingInterval.Max.Duration = 100 * time.Millisecond
	cfg.Fetch.Command = "cat ../../webcast_feed.json"
	cfg.OnLiveStart.Commands = []config.Command{{Command: "echo hi"}}

	f := fetcher.New(cfg.Fetch.Command)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var mu sync.Mutex
	dispatched := map[string]int{}
	d := &countingDispatcher{mu: &mu, dispatched: dispatched}

	m := New(cfg, f, d, logger)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		m.poll(ctx)
	}

	mu.Lock()
	defer mu.Unlock()
	for roomID, count := range dispatched {
		if count != 1 {
			t.Errorf("room %s dispatched %d times, want 1", roomID, count)
		}
	}
	t.Logf("dispatched: %v", dispatched)
}

type countingDispatcher struct {
	mu         *sync.Mutex
	dispatched map[string]int
}

func (d *countingDispatcher) RunLive(ctx context.Context, lv live.Live, commands []config.Command,
	lookup func(roomID string) (live.Live, int, bool)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dispatched[lv.RoomID]++
}
