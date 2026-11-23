package main

import (
	"context"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/bootstrap"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() { os.Exit(run()) }
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return bootstrap.RunSemesterMaintenance(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
