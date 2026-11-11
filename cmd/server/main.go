package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rustdesk-manager/internal/config"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/server"
	"rustdesk-manager/internal/version"
)

func main() {
	// Commands that need no configuration or database.
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("rustdesk-manager", version.Version)
		return
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	setupLogging(cfg.LogLevel)

	gdb, err := db.Open(cfg)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		os.Exit(runCommand(gdb, os.Args[1:]))
	}
	if err := db.Bootstrap(gdb, cfg); err != nil {
		slog.Error("bootstrap", "err", err)
		os.Exit(1)
	}

	app := server.New(cfg, gdb)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app.StartBackground(ctx)
	go func() {
		slog.Info("listening", "version", version.Version, "addr", cfg.Listen, "db", cfg.DBDriver)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "err", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	slog.Info("stopped")
}

func setupLogging(level string) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: l})))
}
