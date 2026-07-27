package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/OmniTrustILM/cbom-repository/internal/env"
	"github.com/OmniTrustILM/cbom-repository/internal/health"
	internalHttp "github.com/OmniTrustILM/cbom-repository/internal/http"
	"github.com/OmniTrustILM/cbom-repository/internal/log"
	"github.com/OmniTrustILM/cbom-repository/internal/service"
	"github.com/OmniTrustILM/cbom-repository/internal/store"
)

var version = "dev"

func main() {
	// get configuration from environment variables
	cfg, err := env.New()
	if err != nil {
		panic(err)
	}
	initializeLogging(cfg.LogLevel)
	slog.Info("Starting service 'CBOM-Repository'.", slog.String("version", version))
	slog.Debug("Service configuration read from environment variables.")

	s3Client, s3Manager, err := store.ConnectS3(context.Background(), cfg.Store)
	if err != nil {
		slog.Error("Connecting to backend store failed.", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Debug("Connected to backend store.")

	store := store.New(cfg.Store, s3Client, s3Manager)
	svc, err := service.New(store, cfg.Service)
	if err != nil {
		slog.Error("Initializing service layer failed.", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Debug("Service layer initialized.")

	// Initialize health service with storage checker
	storageChecker := health.NewStorageChecker(store)
	healthSvc := health.NewService(storageChecker)
	slog.Debug("Health service initialized.")

	srv := internalHttp.New(cfg.Http, svc, healthSvc)
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Http.Port),
		Handler: srv.Handler(),
	}

	slog.Info("Starting http server.", slog.Int("port", cfg.Http.Port))

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("`ListenAndServer()` failed.", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func initializeLogging(level slog.Level) {
	base := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: false,
		Level:     level,
	})
	ctxHandler := log.New(base)
	slog.SetDefault(slog.New(ctxHandler))
}
