package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tiktok-agent/internal/cli"
	"tiktok-agent/internal/config"
	"tiktok-agent/internal/fetcher"
	"tiktok-agent/internal/monitor"
	"tiktok-agent/internal/runner"
	"tiktok-agent/internal/status"
	"tiktok-agent/internal/webui"
)

func main() {
	configPath := flag.String("config", "config.yaml", "設定ファイルのパス")
	cliUI := flag.Bool("cli", false, "CLI 画面（一定間隔で画面を更新）を表示する")
	listen := flag.String("listen", "", "WebUI を提供する HTTP アドレス（例: :8080）")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "設定ファイルの読み込みに失敗しました: %v\n", err)
		os.Exit(1)
	}

	store := status.New()

	// CLI 画面が stdout を占有するため、その場合はログをストアのみに記録する
	var fallback slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if *cliUI {
		fallback = nil
	}
	logger := slog.New(status.NewHandler(store, fallback))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	f := fetcher.New(cfg.Fetch.Command)
	r := runner.New(logger, store)
	m := monitor.New(cfg, f, r, logger)
	m.SetStore(store)

	logger.Info("監視を開始します",
		"poll_min", cfg.PollingInterval.Min.Duration.String(),
		"poll_max", cfg.PollingInterval.Max.Duration.String(),
		"targets", cfg.Targets,
		"commands", len(cfg.OnLiveStart.Commands),
	)

	// WebUI
	if *listen != "" {
		wui, err := webui.New(store, logger)
		if err != nil {
			logger.Error("WebUI の初期化に失敗しました", "error", err)
			os.Exit(1)
		}
		srv := &http.Server{Addr: *listen, Handler: wui.Handler()}
		go func() {
			logger.Info("WebUI を起動します", "addr", *listen)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("WebUI がエラーで終了しました", "error", err)
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
	}

	// CLI 画面
	if *cliUI {
		go func() {
			if err := cli.Run(ctx, store); err != nil {
				logger.Error("CLI 画面がエラーで終了しました", "error", err)
			}
		}()
	}

	if err := m.Run(ctx); err != nil {
		logger.Error("監視ループがエラーで終了しました", "error", err)
		os.Exit(1)
	}

	// 起動中のコマンドの完了を待つ
	done := make(chan struct{})
	go func() {
		r.WaitDone()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		logger.Warn("終了待ちがタイムアウトしました。実行中のコマンドが残っています")
	}
	logger.Info("監視を終了しました")
}
