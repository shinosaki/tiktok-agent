package monitor

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"tiktok-agent/internal/config"
	"tiktok-agent/internal/live"
	"tiktok-agent/internal/status"
)

type fakeDispatcher struct {
	dispatched []live.Live
}

func (f *fakeDispatcher) RunLive(ctx context.Context, lv live.Live, commands []config.Command,
	lookup func(roomID string) (live.Live, int, bool)) {
	f.dispatched = append(f.dispatched, lv)
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	c := &config.Config{}
	c.PollingInterval.Min.Duration = 60 * time.Second
	c.PollingInterval.Max.Duration = 120 * time.Second
	c.Fetch.Command = "curl"
	c.Targets = []string{"alice"}
	c.OnLiveStart.Commands = []config.Command{{Command: "echo hi"}}
	return c
}

func newTestMonitor(t *testing.T, c *config.Config) (*Monitor, *fakeDispatcher) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &fakeDispatcher{}
	m := New(c, nil, d, logger)
	return m, d
}

func TestPollDetectsOnceAndPrunes(t *testing.T) {
	c := testConfig(t)
	m, d := newTestMonitor(t, c)

	lv := live.Live{RoomID: "1", Username: "alice", StreamURL: "http://x/1.flv"}

	// 1回目: 新規検出 → ディスパッチ
	m.SetFetchFn(func(ctx context.Context) ([]live.Live, error) {
		return []live.Live{lv}, nil
	})
	m.poll(context.Background())
	if len(d.dispatched) != 1 {
		t.Fatalf("expected 1 dispatch, got %d", len(d.dispatched))
	}

	// 2回目: 同じライブが残っていても再ディスパッチしない
	m.poll(context.Background())
	if len(d.dispatched) != 1 {
		t.Fatalf("expected still 1 dispatch, got %d", len(d.dispatched))
	}

	// ライブが消える → 次に現れたら再ディスパッチ
	m.SetFetchFn(func(ctx context.Context) ([]live.Live, error) {
		return nil, nil
	})
	m.poll(context.Background())

	m.SetFetchFn(func(ctx context.Context) ([]live.Live, error) {
		return []live.Live{lv}, nil
	})
	m.poll(context.Background())
	if len(d.dispatched) != 2 {
		t.Fatalf("expected 2 dispatches, got %d", len(d.dispatched))
	}
}

func TestPollTargetsFilter(t *testing.T) {
	c := testConfig(t)
	m, d := newTestMonitor(t, c)

	m.SetFetchFn(func(ctx context.Context) ([]live.Live, error) {
		return []live.Live{
			{RoomID: "1", Username: "alice", StreamURL: "http://x/1.flv"},
			{RoomID: "2", Username: "bob", StreamURL: "http://x/2.flv"},
		}, nil
	})
	m.poll(context.Background())
	if len(d.dispatched) != 1 || d.dispatched[0].RoomID != "1" {
		t.Fatalf("expected only alice dispatched, got %v", d.dispatched)
	}
}

func TestPollEmptyTargetsIncludesAll(t *testing.T) {
	c := testConfig(t)
	c.Targets = nil
	m, d := newTestMonitor(t, c)

	m.SetFetchFn(func(ctx context.Context) ([]live.Live, error) {
		return []live.Live{
			{RoomID: "1", Username: "alice", StreamURL: "http://x/1.flv"},
			{RoomID: "2", Username: "bob", StreamURL: "http://x/2.flv"},
		}, nil
	})
	m.poll(context.Background())
	if len(d.dispatched) != 2 {
		t.Fatalf("expected 2 dispatches, got %d", len(d.dispatched))
	}
}

func TestPollReportsLivesToStore(t *testing.T) {
	c := testConfig(t)
	m, _ := newTestMonitor(t, c)
	s := status.New()
	m.SetStore(s)

	m.SetFetchFn(func(ctx context.Context) ([]live.Live, error) {
		return []live.Live{
			{RoomID: "1", Username: "alice"},
			{RoomID: "2", Username: "bob"},
		}, nil
	})
	m.poll(context.Background())

	lives := s.Lives()
	if len(lives) != 1 || lives[0].RoomID != "1" {
		t.Fatalf("expected only alice reported, got %v", lives)
	}

	// ライブが消えたらストアからも消える
	m.SetFetchFn(func(ctx context.Context) ([]live.Live, error) {
		return nil, nil
	})
	m.poll(context.Background())
	if got := len(s.Lives()); got != 0 {
		t.Fatalf("expected 0 lives after prune, got %d", got)
	}
}
