package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config はアプリの全体設定を表す。
type Config struct {
	PollingInterval Interval    `yaml:"polling_interval"`
	Fetch           Fetch       `yaml:"fetch"`
	Targets         []string    `yaml:"targets"`
	OnLiveStart     OnLiveStart `yaml:"on_live_start"`
}

// Interval はポーリング間隔のランダム決定範囲を表す。
type Interval struct {
	Min Duration `yaml:"min"`
	Max Duration `yaml:"max"`
}

// Fetch は API 取得方法を表す。
type Fetch struct {
	Command string `yaml:"command"`
}

// OnLiveStart はライブ検出時の実行コマンド設定を表す。
type OnLiveStart struct {
	Commands []Command `yaml:"commands"`
}

// Command は実行するコマンドとリトライ設定を表す。
type Command struct {
	Command string `yaml:"command"`
	Retry   *Retry `yaml:"retry"`
}

// Retry はコマンドのリトライ設定を表す。
type Retry struct {
	Enabled    bool     `yaml:"enabled"`
	MaxRetries int      `yaml:"max_retries"`
	Interval   Interval `yaml:"interval"`
}

// UnmarshalYAML は旧形式（プレーン文字列）と新形式（command/retry のマッピング）の両方を受け付ける。
func (c *Command) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var s string
		if err := node.Decode(&s); err != nil {
			return err
		}
		c.Command = s
		c.Retry = nil
		return nil
	}
	type raw Command
	var r raw
	if err := node.Decode(&r); err != nil {
		return err
	}
	*c = Command(r)
	return nil
}

// Duration は YAML 上で "60s" のように記述する time.Duration。
type Duration struct {
	time.Duration
}

// UnmarshalYAML は "60s" 形式の文字列を time.Duration に変換する。
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = v
	return nil
}

// Load は設定ファイルを読み込み、検証して返す。
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate は設定値を検証する。
func (c *Config) Validate() error {
	if c.PollingInterval.Min.Duration <= 0 {
		return errors.New("polling_interval.min must be positive")
	}
	if c.PollingInterval.Max.Duration < c.PollingInterval.Min.Duration {
		return errors.New("polling_interval.max must be >= polling_interval.min")
	}
	if c.Fetch.Command == "" {
		return errors.New("fetch.command is required")
	}
	if len(c.OnLiveStart.Commands) == 0 {
		return errors.New("on_live_start.commands must have at least one command")
	}
	for i, cmd := range c.OnLiveStart.Commands {
		if cmd.Command == "" {
			return fmt.Errorf("on_live_start.commands[%d].command is required", i)
		}
		if cmd.Retry == nil || !cmd.Retry.Enabled {
			continue
		}
		if cmd.Retry.MaxRetries <= 0 {
			return fmt.Errorf("on_live_start.commands[%d].retry.max_retries must be positive", i)
		}
		if cmd.Retry.Interval.Min.Duration <= 0 {
			return fmt.Errorf("on_live_start.commands[%d].retry.interval.min must be positive", i)
		}
		if cmd.Retry.Interval.Max.Duration < cmd.Retry.Interval.Min.Duration {
			return fmt.Errorf("on_live_start.commands[%d].retry.interval.max must be >= interval.min", i)
		}
	}
	return nil
}
