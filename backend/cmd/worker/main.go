package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/bootstrap"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bootstrap.RunWorker(ctx, logger); err != nil {
		logger.Error("Tutti worker could not start", "error", err.Error())
		os.Exit(1)
	}
}
