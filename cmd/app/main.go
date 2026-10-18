package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/RaniXst/clothing-store/internal/config"
	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/internal/middleware"
	"github.com/RaniXst/clothing-store/internal/photo"
	"github.com/RaniXst/clothing-store/internal/product"
	"github.com/RaniXst/clothing-store/internal/storage"
	"github.com/RaniXst/clothing-store/migrations"
)

func main() {
	startupLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(); err != nil {
		startupLogger.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := database.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := migrations.Apply(ctx, db); err != nil {
		return err
	}

	fileStore, err := storage.NewLocal(cfg.UploadDir)
	if err != nil {
		return err
	}
	productRepository := product.NewSQLRepository(db)
	photoRepository := photo.NewSQLRepository(db)
	mutationMu := &sync.Mutex{}
	productService := product.NewService(productRepository, photoRepository, fileStore, mutationMu)
	photoService := photo.NewService(photoRepository, productRepository, fileStore, mutationMu)
	if err := photoService.Recover(ctx); err != nil {
		return err
	}

	router := chi.NewRouter()
	router.Use(middleware.Logging(logger))
	product.NewHandler(productService).RegisterRoutes(router)
	photo.NewHandler(photoService).RegisterRoutes(router)
	// These write routes are intentionally unauthenticated until stage 4. Run this
	// service only on a trusted local development network in the meantime.
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(checkCtx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})

	server := &http.Server{Addr: net.JoinHostPort("127.0.0.1", cfg.AppPort), Handler: router, ReadHeaderTimeout: 5 * time.Second}
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("starting HTTP server", "port", cfg.AppPort)
		serverErr <- server.ListenAndServe()
	}()
	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}

func logLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
