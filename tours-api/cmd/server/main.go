// Command server runs the Old Town Montgomery tours HTTP API.
//
// Configuration is via environment variables (see internal/config).
//
// Graceful shutdown: SIGINT / SIGTERM triggers Shutdown with a
// 15-second drain window so in-flight uploads can finish.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/landmarks-foundation/tours-api/internal/api"
	"github.com/landmarks-foundation/tours-api/internal/config"
	"github.com/landmarks-foundation/tours-api/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Error("config error", "err", err)
		os.Exit(1)
	}

	sites, err := storage.NewLocalSiteStore(cfg.DataDir)
	if err != nil {
		logger.Error("init site store", "err", err)
		os.Exit(1)
	}
	media, err := storage.NewLocalMediaStore(cfg.DataDir)
	if err != nil {
		logger.Error("init media store", "err", err)
		os.Exit(1)
	}

	// Serve the static admin UI from ./web if it exists.
	// Set WEB_ROOT="" (or remove the directory) to disable.
	webRoot := os.Getenv("WEB_ROOT")
	if webRoot == "" {
		if _, err := os.Stat("./web"); err == nil {
			webRoot = "./web"
		}
	}

	srv := api.NewServer(cfg, logger, sites, media, webRoot)

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		// No ReadTimeout/WriteTimeout on the whole request: uploads
		// of large audio/video files would otherwise be killed.
		IdleTimeout: 120 * time.Second,
	}

	// Run the server in its own goroutine so main can listen for signals.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server starting",
			"port", cfg.Port,
			"data_dir", cfg.DataDir,
			"web_root", webRoot,
			"max_upload_mb", cfg.MaxUploadBytes/(1024*1024),
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Wait for either an unexpected server error or a shutdown signal.
	shutdownSig := make(chan os.Signal, 1)
	signal.Notify(shutdownSig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		logger.Error("server failed", "err", err)
		os.Exit(1)
	case sig := <-shutdownSig:
		logger.Info("shutdown signal received", "signal", sig.String())
	}

	// Give in-flight requests up to 15s to complete.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	logger.Info("server stopped cleanly")
}
