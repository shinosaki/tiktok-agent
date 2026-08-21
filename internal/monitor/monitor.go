package monitor

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"tiktok-agent/internal/config"
	"tiktok-agent/internal/fetcher"
	"tiktok-agent/internal/live"
	"tiktok-agent/internal/status"
)

// Dispatcher は検出したライブのコマンド実行を引き受ける。
type Dispatcher interface {
	RunLive(ctx context.Context, lv live.Live, commands []config.Command,
		lookup func(roomID string) (live.Live, int, bool))
}

// Monitor はポーリングループでライブを検出し、コマンド実行をディスパッチする。
type Monitor struct {
	cfg     *config.Config
	fetcher *fetcher.Fetcher
	runner  Dispatcher
	logger  *slog.Logger

	mu sync.Mutex
	// seen は検出済みかつ現在もアクティブなライブ ID。
	seen map[string]struct{}
	// active は最新ポーリング結果のライブデータ。リトライ時に最新データを取得するために保持する。
	active map[string]live.Live
	// gen はライブの世代番号。ライブが消えて再検出されるとインクリメントされ、
	// 古いリトライループを無効化する（二重実行防止）。
	gen map[string]int

	// store はアクティブなライブの表示用ストア（nil なら報告しない）。
	store *status.Store

	// fetchFn はテスト用に差し替え可能なフェッチ関数。
	fetchFn func(ctx context.Context) ([]live.Live, error)
}

// New は Monitor を生成する。
func New(cfg *config.Config, f *fetcher.Fetcher, r Dispatcher, logger *slog.Logger) *Monitor {
	return &Monitor{
		cfg:     cfg,
		fetcher: f,
		runner:  r,
		logger:  logger,
		seen:    make(map[string]struct{}),
		active:  make(map[string]live.Live),
		gen:     make(map[string]int),
	}
}

// SetFetchFn はテスト用にフェッチ関数を差し替える。
func (m *Monitor) SetFetchFn(fn func(ctx context.Context) ([]live.Live, error)) {
	m.fetchFn = fn
}

// SetStore はアクティブなライブを報告するストアを設定する。
func (m *Monitor) SetStore(s *status.Store) {
	m.store = s
}

// Run は設定した間隔でポーリングを継続する。ctx がキャンセルされると終了する。
func (m *Monitor) Run(ctx context.Context) error {
	for {
		m.poll(ctx)
		if ctx.Err() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(m.nextInterval()):
		}
	}
}

// poll は 1 回分のポーリングを行う。
func (m *Monitor) poll(ctx context.Context) {
	fetchFn := m.fetchFn
	if fetchFn == nil {
		fetchFn = m.fetcher.FetchLives
	}
	lives, err := fetchFn(ctx)
	if err != nil {
		m.logger.Warn("fetch failed", "error", err)
		return
	}

	var toDispatch []live.Live
	current := make(map[string]live.Live, len(lives))
	m.mu.Lock()
	for _, lv := range lives {
		if !m.targeted(lv) {
			continue
		}
		current[lv.RoomID] = lv
		m.active[lv.RoomID] = lv
		if m.isNewLocked(lv.RoomID) {
			m.gen[lv.RoomID]++
			toDispatch = append(toDispatch, lv)
		}
	}
	// ポーリング結果から消えたライブ ID は即削除する
	for id := range m.seen {
		if _, ok := current[id]; !ok {
			delete(m.seen, id)
			delete(m.active, id)
		}
	}
	m.mu.Unlock()

	if m.store != nil {
		targeted := make([]live.Live, 0, len(current))
		for _, lv := range current {
			targeted = append(targeted, lv)
		}
		m.store.SetLives(targeted)
	}

	for _, lv := range toDispatch {
		if lv.StreamURL == "" {
			m.logger.Warn("live without stream url", "room_id", lv.RoomID)
		}
		m.logger.Info("live detected",
			"room_id", lv.RoomID, "username", lv.Username,
			"nickname", lv.Nickname, "title", lv.Title)
		m.runner.RunLive(ctx, lv, m.cfg.OnLiveStart.Commands, m.Lookup)
	}
}

// Lookup は最新ポーリング結果のライブデータと世代番号を返す。存在しなければ ok=false。
// リトライ時にアクティブ判定と最新データ取得に使用する。
func (m *Monitor) Lookup(roomID string) (live.Live, int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lv, ok := m.active[roomID]
	return lv, m.gen[roomID], ok
}

// targeted は対象ユーザに含まれるか判定する。targets 未指定時は全ライブを対象とする。
func (m *Monitor) targeted(lv live.Live) bool {
	if len(m.cfg.Targets) == 0 {
		return true
	}
	for _, t := range m.cfg.Targets {
		if t == lv.Username {
			return true
		}
	}
	return false
}

// isNewLocked は未検出のライブ ID か判定し、検出済みとして記録する。呼び出し側で m.mu のロックが必須。
func (m *Monitor) isNewLocked(id string) bool {
	if _, ok := m.seen[id]; ok {
		return false
	}
	m.seen[id] = struct{}{}
	return true
}

// nextInterval は設定範囲からランダムなポーリング間隔を決定する。
func (m *Monitor) nextInterval() time.Duration {
	min := m.cfg.PollingInterval.Min.Duration
	max := m.cfg.PollingInterval.Max.Duration
	if max <= min {
		return min
	}
	return min + time.Duration(rand.Int64N(int64(max-min)))
}
