package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
polling_interval:
  min: 60s
  max: 120s
fetch:
  command: |
    curl 'https://example.com' -b 'sessionid=x'
targets:
  - alice
on_live_start:
  commands:
    - echo "{{.RoomID}}"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if c.PollingInterval.Min.Duration.String() != "1m0s" {
		t.Errorf("unexpected Min: %s", c.PollingInterval.Min.Duration)
	}
	if c.PollingInterval.Max.Duration.String() != "2m0s" {
		t.Errorf("unexpected Max: %s", c.PollingInterval.Max.Duration)
	}
	if len(c.Targets) != 1 || c.Targets[0] != "alice" {
		t.Errorf("unexpected Targets: %v", c.Targets)
	}
	if len(c.OnLiveStart.Commands) != 1 {
		t.Errorf("unexpected Commands: %v", c.OnLiveStart.Commands)
	}
}

func TestLoadExample(t *testing.T) {
	c, err := Load("../../config.example.yaml")
	if err != nil {
		t.Fatalf("config.example.yaml should load: %v", err)
	}
	if len(c.OnLiveStart.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %v", c.OnLiveStart.Commands)
	}
	for _, cmd := range c.OnLiveStart.Commands {
		if cmd.Command == "" {
			t.Fatal("command should not be empty")
		}
	}
}

func TestLoadWithRetry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
polling_interval:
  min: 60s
  max: 120s
fetch:
  command: |
    curl 'https://example.com' -b 'sessionid=x'
targets:
  - alice
on_live_start:
  commands:
    - command: |
        ffmpeg -i "{{.StreamURL}}" /tmp/{{.RoomID}}.ts
      retry:
        enabled: true
        max_retries: 3
        interval:
          min: 30s
          max: 60s
    - echo "{{.RoomID}}"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(c.OnLiveStart.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(c.OnLiveStart.Commands))
	}

	retryCmd := c.OnLiveStart.Commands[0]
	if retryCmd.Command == "" {
		t.Fatal("retry command should not be empty")
	}
	if retryCmd.Retry == nil || !retryCmd.Retry.Enabled {
		t.Fatal("retry should be enabled")
	}
	if retryCmd.Retry.MaxRetries != 3 {
		t.Errorf("unexpected MaxRetries: %d", retryCmd.Retry.MaxRetries)
	}
	if retryCmd.Retry.Interval.Min.Duration != 30*time.Second {
		t.Errorf("unexpected Min: %s", retryCmd.Retry.Interval.Min.Duration)
	}
	if retryCmd.Retry.Interval.Max.Duration != 60*time.Second {
		t.Errorf("unexpected Max: %s", retryCmd.Retry.Interval.Max.Duration)
	}

	plainCmd := c.OnLiveStart.Commands[1]
	if plainCmd.Retry != nil {
		t.Fatal("plain command should have no retry")
	}
}

func TestLoadRetry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
polling_interval:
  min: 60s
  max: 120s
fetch:
  command: curl x
on_live_start:
  commands:
    - command: ffmpeg -i "{{.StreamURL}}" out.ts
      retry:
        enabled: true
        max_retries: 3
        interval:
          min: 10s
          max: 30s
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	cmd := c.OnLiveStart.Commands[0]
	if cmd.Command != `ffmpeg -i "{{.StreamURL}}" out.ts` {
		t.Fatalf("unexpected Command: %q", cmd.Command)
	}
	if cmd.Retry == nil || !cmd.Retry.Enabled || cmd.Retry.MaxRetries != 3 {
		t.Fatalf("unexpected Retry: %+v", cmd.Retry)
	}
	if cmd.Retry.Interval.Min.Duration != 10*time.Second ||
		cmd.Retry.Interval.Max.Duration != 30*time.Second {
		t.Fatalf("unexpected interval: %+v", cmd.Retry.Interval)
	}
}

func TestValidateRetry(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"max_retries zero", "polling_interval:\n  min: 60s\n  max: 120s\nfetch:\n  command: x\non_live_start:\n  commands:\n    - command: a\n      retry:\n        enabled: true\n        max_retries: 0\n        interval: {min: 1s, max: 2s}", true},
		{"interval min zero", "polling_interval:\n  min: 60s\n  max: 120s\nfetch:\n  command: x\non_live_start:\n  commands:\n    - command: a\n      retry:\n        enabled: true\n        max_retries: 1\n        interval: {min: 0s, max: 2s}", true},
		{"valid", "polling_interval:\n  min: 60s\n  max: 120s\nfetch:\n  command: x\non_live_start:\n  commands:\n    - command: a\n      retry:\n        enabled: true\n        max_retries: 2\n        interval: {min: 1s, max: 2s}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"min zero", "polling_interval:\n  min: 0s\n  max: 60s\nfetch:\n  command: x\non_live_start:\n  commands: [a]", true},
		{"max less than min", "polling_interval:\n  min: 120s\n  max: 60s\nfetch:\n  command: x\non_live_start:\n  commands: [a]", true},
		{"no fetch command", "polling_interval:\n  min: 60s\n  max: 120s\nfetch:\n  command: ''\non_live_start:\n  commands: [a]", true},
		{"no commands", "polling_interval:\n  min: 60s\n  max: 120s\nfetch:\n  command: x\non_live_start:\n  commands: []", true},
		{"retry max_retries zero", "polling_interval:\n  min: 60s\n  max: 120s\nfetch:\n  command: x\non_live_start:\n  commands:\n    - command: a\n      retry:\n        enabled: true\n        max_retries: 0\n        interval:\n          min: 30s\n          max: 60s", true},
		{"retry interval max less than min", "polling_interval:\n  min: 60s\n  max: 120s\nfetch:\n  command: x\non_live_start:\n  commands:\n    - command: a\n      retry:\n        enabled: true\n        max_retries: 3\n        interval:\n          min: 60s\n          max: 30s", true},
		{"retry valid", "polling_interval:\n  min: 60s\n  max: 120s\nfetch:\n  command: x\non_live_start:\n  commands:\n    - command: a\n      retry:\n        enabled: true\n        max_retries: 3\n        interval:\n          min: 30s\n          max: 60s", false},
		{"valid", "polling_interval:\n  min: 60s\n  max: 120s\nfetch:\n  command: x\non_live_start:\n  commands: [a]", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
