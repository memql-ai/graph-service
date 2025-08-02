package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/memql/graph-service/internal/api"
	"github.com/memql/graph-service/internal/kafka"
	"github.com/memql/graph-service/internal/neo4j"
)

func main() {
	// Initialize logger
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Load configuration
	if err := loadConfig(); err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Neo4j client
	neo4jClient, err := neo4j.NewClient(
		viper.GetString("neo4j.uri"),
		viper.GetString("neo4j.username"),
		viper.GetString("neo4j.password"),
		logger,
	)
	if err != nil {
		logger.Fatal("failed to create Neo4j client", zap.Error(err))
	}
	defer neo4jClient.Close(ctx)

	// Initialize Kafka producer
	producer, err := kafka.NewProducer(
		viper.GetString("kafka.brokers"),
		logger,
	)
	if err != nil {
		logger.Fatal("failed to create Kafka producer", zap.Error(err))
	}
	defer producer.Close()

	// Initialize Kafka consumer
	consumer, err := kafka.NewConsumer(
		viper.GetString("kafka.brokers"),
		viper.GetString("kafka.consumer_group"),
		neo4jClient,
		producer,
		logger,
	)
	if err != nil {
		logger.Fatal("failed to create Kafka consumer", zap.Error(err))
	}

	// Start consumer in background
	go func() {
		topics := []string{
			"tenant-*-entities",
			"tenant-*-memories",
		}
		if err := consumer.Start(ctx, topics); err != nil {
			logger.Error("consumer error", zap.Error(err))
		}
	}()

	// Create API handler
	handler := api.NewHandler(neo4jClient, producer, logger)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Ready check
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		// Check Neo4j connectivity - try a simple query
		query := "RETURN 1"
		_, err := neo4jClient.ExecuteQuery(ctx, "system", query, nil)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Neo4j not ready"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ready"))
	})

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		handler.RegisterRoutes(r)
	})

	// Prometheus metrics
	r.Handle("/metrics", promhttp.Handler())

	// Create HTTP server
	srv := &http.Server{
		Addr:         viper.GetString("server.address"),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	go func() {
		logger.Info("starting server", zap.String("address", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("failed to start server", zap.Error(err))
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down server")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop consumer
	if err := consumer.Stop(); err != nil {
		logger.Error("failed to stop consumer", zap.Error(err))
	}

	// Shutdown HTTP server
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown server", zap.Error(err))
	}

	logger.Info("server stopped")
}

func loadConfig() error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath(".")

	// Set defaults
	viper.SetDefault("server.address", ":8080")
	viper.SetDefault("neo4j.uri", "bolt://localhost:7687")
	viper.SetDefault("neo4j.username", "neo4j")
	viper.SetDefault("neo4j.password", "password")
	viper.SetDefault("kafka.brokers", "localhost:9092")
	viper.SetDefault("kafka.consumer_group", "graph-service")

	// Read environment variables
	viper.AutomaticEnv()
	viper.SetEnvPrefix("GRAPH")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("failed to read config: %w", err)
		}
	}

	return nil
}