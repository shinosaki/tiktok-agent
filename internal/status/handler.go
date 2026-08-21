package status

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
)

// Handler は slog のログを Store に記録しつつ、必要に応じて下流のハンドラへ転送する。
type Handler struct {
	store    *Store
	fallback slog.Handler
	attrs    []slog.Attr
	groups   []string
}

// NewHandler は Store へログを記録する slog.Handler を生成する。
// fallback が nil の場合は記録のみ行う（CLI 画面表示時など stdout を占有する場合）。
func NewHandler(store *Store, fallback slog.Handler) *Handler {
	return &Handler{store: store, fallback: fallback}
}

// Enabled は常に有効にする。
func (h *Handler) Enabled(context.Context, slog.Level) bool { return true }

// Handle はログを TextHandler 形式でフォーマットし、Store に記録して転送する。
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	r2 := r
	r2.AddAttrs(h.attrs...)
	var buf bytes.Buffer
	var th slog.Handler = slog.NewTextHandler(&buf, nil)
	for _, g := range h.groups {
		th = th.WithGroup(g)
	}
	if err := th.Handle(ctx, r2); err != nil {
		return err
	}
	h.store.AddLog(strings.TrimRight(buf.String(), "\n"))
	if h.fallback != nil {
		return h.fallback.Handle(ctx, r)
	}
	return nil
}

// WithAttrs は属性付きハンドラを返す。
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		store:    h.store,
		fallback: h.fallback,
		attrs:    append(append([]slog.Attr(nil), h.attrs...), attrs...),
		groups:   append([]string(nil), h.groups...),
	}
}

// WithGroup はグループ付きハンドラを返す。
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{
		store:    h.store,
		fallback: h.fallback,
		attrs:    append([]slog.Attr(nil), h.attrs...),
		groups:   append(append([]string(nil), h.groups...), name),
	}
}
