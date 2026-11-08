package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"jusdoit/internal/httpapi"
	"jusdoit/internal/store"
)

var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	addr := env("JUSDOIT_ADDR", "127.0.0.1:8000")
	dbPath := env("JUSDOIT_DB", "jusdoit.db")
	if v := env("JUSDOIT_VERSION", ""); v != "" {
		version = v
	}

	st, err := store.Open(dbPath)
	if err != nil {
		slog.Error("database", "err", err, "path", dbPath)
		os.Exit(1)
	}
	defer st.Close()

	srv := &http.Server{
		Addr:              addr,
		Handler:           (&httpapi.Server{Store: st, Version: version}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("listening", "addr", addr, "db", dbPath, "version", version)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown", "err", err)
	}
	slog.Info("stopped")
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
