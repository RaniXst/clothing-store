package main

import (
	"context"
	"database/sql"
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

	"github.com/RaniXst/clothing-store/internal/auth"
	"github.com/RaniXst/clothing-store/internal/config"
	"github.com/RaniXst/clothing-store/internal/database"
	"github.com/RaniXst/clothing-store/internal/middleware"
	"github.com/RaniXst/clothing-store/internal/photo"
	"github.com/RaniXst/clothing-store/internal/product"
	"github.com/RaniXst/clothing-store/internal/storage"
	"github.com/RaniXst/clothing-store/internal/user"
	"github.com/RaniXst/clothing-store/migrations"
)

func main() {
	startupLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "bootstrap-admin" {
			startupLogger.Error("unknown command", "usage", "app [bootstrap-admin]")
			os.Exit(2)
		}
		if err := bootstrapAdmin(); err != nil {
			startupLogger.Error("administrator bootstrap failed", "error", err)
			os.Exit(1)
		}
		return
	}
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
	router, err := buildRouter(ctx, logger, db, cfg.UploadDir, cfg.AuthCookieSecure)
	if err != nil {
		return err
	}
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

func buildRouter(ctx context.Context, logger *slog.Logger, db *sql.DB, uploadDir string, secureCookie bool) (chi.Router, error) {
	fileStore, err := storage.NewLocal(uploadDir)
	if err != nil {
		return nil, err
	}
	productRepository := product.NewSQLRepository(db)
	photoRepository := photo.NewSQLRepository(db)
	userRepository := user.NewSQLRepository(db)
	userService := user.NewService(userRepository, auth.PasswordHasher{})
	authService, err := auth.NewService(userRepository, auth.NewSQLSessionRepository(db))
	if err != nil {
		return nil, err
	}
	mutationMu := &sync.Mutex{}
	productService := product.NewService(productRepository, photoRepository, fileStore, mutationMu)
	photoService := photo.NewService(photoRepository, productRepository, fileStore, mutationMu)
	if err := photoService.Recover(ctx); err != nil {
		return nil, err
	}

	router := chi.NewRouter()
	router.Use(middleware.Logging(logger))
	productHandler := product.NewHandler(productService)
	photoHandler := photo.NewHandler(photoService)
	userHandler := user.NewHandler(userService)
	authHandler := auth.NewHandler(authService, auth.NewLoginLimiter(10, 15*time.Minute), secureCookie)
	productHandler.RegisterReadRoutes(router)
	photoHandler.RegisterReadRoutes(router)
	userHandler.RegisterPublicRoutes(router)
	authHandler.RegisterPublicRoutes(router)
	router.Group(func(protected chi.Router) {
		protected.Use(authService.RequireSession)
		authHandler.RegisterProtectedRoutes(protected)
		protected.Group(func(admin chi.Router) {
			admin.Use(auth.RequireRoles("admin"))
			admin.Use(auth.CSRF)
			userHandler.RegisterAdminRoutes(admin)
			productHandler.RegisterWriteRoutes(admin)
			photoHandler.RegisterWriteRoutes(admin)
		})
	})
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
	return router, nil
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
