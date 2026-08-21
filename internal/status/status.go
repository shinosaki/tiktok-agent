package status

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"tiktok-agent/internal/live"
)

const (
	// defaultOutputLines はコマンドの stdout/stderr に保持する最大行数。
	defaultOutputLines = 2000
	// defaultLogLines はログに保持する最大行数。
	defaultLogLines = 500
)

// Live は現在処理中のライブの表示用データ。
type Live struct {
	RoomID      string    `json:"room_id"`
	Username    string    `json:"username"`
	Nickname    string    `json:"nickname"`
	Title       string    `json:"title"`
	StreamURL   string    `json:"stream_url"`
	ViewerCount int       `json:"viewer_count"`
	LikeCount   int64     `json:"like_count"`
	DetectedAt  time.Time `json:"detected_at"`
}

// State はコマンドの実行状態。
type State string

const (
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

// Command は起動したコマンドの記録。
type Command struct {
	ID          string      `json:"id"`
	RoomID      string      `json:"room_id"`
	Username    string      `json:"username"`
	CommandLine string      `json:"command_line"`
	StartedAt   time.Time   `json:"started_at"`
	FinishedAt  *time.Time  `json:"finished_at,omitempty"`
	ExitErr     string      `json:"exit_err,omitempty"`
	Stdout      *LineBuffer `json:"stdout"`
	Stderr      *LineBuffer `json:"stderr"`
}

// State はコマンドの現在の実行状態を返す。
func (c *Command) State() State {
	if c.FinishedAt != nil {
		if c.ExitErr != "" {
			return StateFailed
		}
		return StateDone
	}
	return StateRunning
}

// Store は CLI/WebUI が参照する共通の状態ストア。
type Store struct {
	mu       sync.Mutex
	lives    map[string]Live
	commands []*Command
	logs     *LineBuffer
	nextID   uint64
}

// New は Store を生成する。
func New() *Store {
	return &Store{
		lives: make(map[string]Live),
		logs:  NewLineBuffer(defaultLogLines),
	}
}

// SetLives はアクティブなライブ集合を丸ごと置き換える。
// ポーリング結果から消えたライブは即座に表示から消える。
func (s *Store) SetLives(lives []live.Live) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]Live, len(lives))
	now := time.Now()
	for _, lv := range lives {
		detected := now
		if prev, ok := s.lives[lv.RoomID]; ok {
			detected = prev.DetectedAt
		}
		next[lv.RoomID] = Live{
			RoomID:      lv.RoomID,
			Username:    lv.Username,
			Nickname:    lv.Nickname,
			Title:       lv.Title,
			StreamURL:   lv.StreamURL,
			ViewerCount: lv.ViewerCount,
			LikeCount:   lv.LikeCount,
			DetectedAt:  detected,
		}
	}
	s.lives = next
}

// Lives はアクティブなライブのスナップショットを返す（RoomID 順）。
func (s *Store) Lives() []Live {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Live, 0, len(s.lives))
	for _, lv := range s.lives {
		out = append(out, lv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RoomID < out[j].RoomID })
	return out
}

// CommandStarted はコマンド起動を記録し、記録オブジェクトを返す。
func (s *Store) CommandStarted(lv live.Live, cmdLine string) *Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	c := &Command{
		ID:          fmt.Sprintf("%d", s.nextID),
		RoomID:      lv.RoomID,
		Username:    lv.Username,
		CommandLine: cmdLine,
		StartedAt:   time.Now(),
		Stdout:      NewLineBuffer(defaultOutputLines),
		Stderr:      NewLineBuffer(defaultOutputLines),
	}
	s.commands = append(s.commands, c)
	return c
}

// CommandOutput はコマンドの stdout/stderr に行を追記する。
func (s *Store) CommandOutput(c *Command, isErr bool, line string) {
	if c == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if isErr {
		c.Stderr.Add(line)
	} else {
		c.Stdout.Add(line)
	}
}

// CommandFinished はコマンドの終了を記録する。
func (s *Store) CommandFinished(c *Command, err error) {
	if c == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	c.FinishedAt = &now
	if err != nil {
		c.ExitErr = err.Error()
	}
}

// Commands は起動したコマンドのスナップショットを返す（新しい順）。
func (s *Store) Commands() []Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Command, 0, len(s.commands))
	for i := len(s.commands) - 1; i >= 0; i-- {
		out = append(out, *s.commands[i])
	}
	return out
}

// AddLog はログ行を追記する。
func (s *Store) AddLog(line string) {
	s.logs.Add(line)
}

// Logs はログのスナップショットを返す。
func (s *Store) Logs() []string {
	return s.logs.Snapshot()
}

// LogsLast は最新 n 行のスナップショットを返す。
func (s *Store) LogsLast(n int) []string {
	lines := s.logs.Snapshot()
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// LineBuffer は上限付きの行バッファ。古い行から破棄される。
type LineBuffer struct {
	mu      sync.Mutex
	lines   []string
	max     int
	dropped int
}

// NewLineBuffer は最大 max 行を保持する LineBuffer を生成する。
func NewLineBuffer(max int) *LineBuffer {
	return &LineBuffer{max: max}
}

// Add は行を追記する。
func (b *LineBuffer) Add(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, line)
	if len(b.lines) > b.max {
		over := len(b.lines) - b.max
		b.dropped += over
		b.lines = append([]string(nil), b.lines[over:]...)
	}
}

// Snapshot は保持中の行のコピーを返す。
func (b *LineBuffer) Snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

// Dropped は容量超過で破棄した行数を返す。
func (b *LineBuffer) Dropped() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// MarshalJSON は保持中の行を JSON 配列として出力する。
func (b *LineBuffer) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.Snapshot())
}

// UnmarshalJSON は JSON 配列を読み込む（API 応答の再パース用）。
func (b *LineBuffer) UnmarshalJSON(data []byte) error {
	var lines []string
	if err := json.Unmarshal(data, &lines); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = lines
	b.max = defaultOutputLines
	return nil
}
