package opapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// Server represents the Operational API HTTP server.
type Server struct {
	httpServer       *http.Server
	logger           *logrus.Entry
	modelManager     ModelManagerInterface     // Using interface for flexibility
	inventoryManager InventoryManagerInterface // Interface for InventoryManager
}

// Config holds configuration for the API server.
type Config struct {
	ListenAddress    string
	Logger           *logrus.Logger // Expects an already configured Logrus logger instance
	ModelManager     ModelManagerInterface
	InventoryManager InventoryManagerInterface
	// Other config options like TLS paths, timeouts, etc. can be added here.
}

// NewServer creates a new instance of the API server.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		// Fallback to a default logger if none is provided, though it's recommended
		// to pass a configured logger from the main application.
		defaultLogger := logrus.New()
		defaultLogger.SetFormatter(&logrus.TextFormatter{})
		cfg.Logger = defaultLogger
		cfg.Logger.Warn("No logger provided to opapi.NewServer, using default.")
	}
	loggerEntry := cfg.Logger.WithField("component", "opapi-server")

	router := mux.NewRouter()
	apiV1Router := router.PathPrefix("/api/v1").Subrouter()

	// Health check endpoint
	apiV1Router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Using fmt.Fprintln for simple JSON, consider json.NewEncoder for complex structs
		_, err := fmt.Fprintln(w, `{"status": "ok"}`)
		if err != nil {
			loggerEntry.WithError(err).Error("Failed to write health check response")
		}
		loggerEntry.Debug("Health check endpoint hit")
	}).Methods(http.MethodGet)

	// ModelManager Handlers
	if cfg.ModelManager == nil {
		loggerEntry.Warn("ModelManager not provided in opapi.Config; model endpoints will not be available.")
	} else {
		modelHandlers := NewModelHandlers(cfg.ModelManager, loggerEntry)
		modelHandlers.RegisterRoutes(apiV1Router)
	}

	// InventoryManager Handlers
	if cfg.InventoryManager == nil {
		loggerEntry.Warn("InventoryManager not provided in opapi.Config; inventory endpoints will not be available.")
	} else {
		inventoryHandlers := NewInventoryHandlers(cfg.InventoryManager, loggerEntry)
		inventoryHandlers.RegisterRoutes(apiV1Router)
	}

	// Future: Add handlers for Scaler operations here.

	httpServer := &http.Server{
		Addr:    cfg.ListenAddress,
		Handler: router,
		// Consider adding ReadTimeout, WriteTimeout, IdleTimeout for production robustness
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	return &Server{
		httpServer:       httpServer,
		logger:           loggerEntry,
		modelManager:     cfg.ModelManager,
		inventoryManager: cfg.InventoryManager,
	}, nil
}

// Start runs the HTTP server. This is a blocking call until the server is shutdown.
func (s *Server) Start() error {
	s.logger.Infof("Operational API server starting on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
		// Log the error before returning, as it might not be caught by the caller in a goroutine
		s.logger.WithError(err).Error("HTTP server ListenAndServe error")
		return fmt.Errorf("HTTP server ListenAndServe error: %w", err)
	}
	return nil
}

// Stop gracefully shuts down the server within the given context's deadline.
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("Operational API server stopping...")
	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger.WithError(err).Error("HTTP server shutdown error")
		return fmt.Errorf("HTTP server shutdown error: %w", err)
	}
	s.logger.Info("Operational API server stopped gracefully.")
	return nil
}
