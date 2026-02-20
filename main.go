package main

import (
	"context"
	"log"
	"nexus/internal/handler"
	"nexus/internal/llm"
	"nexus/internal/repository"
	"nexus/internal/usecase"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	hfToken := "REDACTED"
	// authURL := os.Getenv("AUTH_URL")
	llmClient := llm.NewHFClient(llm.HFConfig{Token: hfToken})

	cacheTTL := 15 * time.Minute
	if v := os.Getenv("CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cacheTTL = d
		}
	}

	var repo *repository.Repository
	pgURL := os.Getenv("DATABASE_URL")
	redisAddr := os.Getenv("REDIS_ADDR")
	if pgURL != "" || redisAddr != "" {
		redisDB := 0
		if v := os.Getenv("REDIS_DB"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				redisDB = n
			}
		}
		r, err := repository.NewRepository(context.Background(), repository.Config{
			PostgresURL:   pgURL,
			RedisAddr:     redisAddr,
			RedisPassword: os.Getenv("REDIS_PASSWORD"),
			RedisDB:       redisDB,
		})
		if err != nil {
			log.Fatalf("repository init: %v", err)
		}
		migCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := r.AutoMigrate(migCtx); err != nil {
			cancel()
			log.Fatalf("repository migrate: %v", err)
		}
		cancel()
		repo = r
	}

	analyzer := usecase.NewAnalyzer(llmClient, repo, cacheTTL)
	analyzeHandler := handler.NewAnalyzeHandler(analyzer)
	// authMW := middleware.NewAuthMiddleware(authURL, nil)

	app := fiber.New()
	app.Use(handler.WithCORS())
	// app.Use(authMW.Handler())
	app.Post("/ai/analyze", analyzeHandler.Handler())
	app.Get("/health", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	app.Get("/ready", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	app.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on :%s", port)
		errCh <- app.Listen(":" + port)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatal(err)
	case sig := <-sigCh:
		log.Printf("shutdown signal: %s", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if repo != nil {
			repo.Close()
		}
		if err := app.ShutdownWithContext(ctx); err != nil {
			log.Fatal(err)
		}
	}
}
