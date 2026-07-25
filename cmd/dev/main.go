package main

import (
	"log/slog"
	"os"

	"github.com/leow/go-gedung-peristiwa/internal/web"
)

func main() {
	addr := os.Getenv("DEV_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	slog.Info("starting dev control plane. issa cool!!!", "addr", addr)

	if err := web.NewServer(addr).ListenAndServe(); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
