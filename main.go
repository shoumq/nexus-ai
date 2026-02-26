package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"nexus/internal/handler"
	"nexus/internal/llm"
	"nexus/internal/middleware"
	"nexus/internal/repository"
	"nexus/internal/usecase"
	"nexus/proto/nexusai/v1"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	authpb "auth_service/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	grpcAddr := os.Getenv("GRPC_ADDR")
	if grpcAddr == "" {
		grpcAddr = ":9091"
	}

	dsToken := "sk-c90536d4ff774f2281d8dade3a1acfda"
	authURL := os.Getenv("AUTH_URL")
	authGRPCAddr := os.Getenv("AUTH_GRPC_ADDR")
	if authGRPCAddr == "" {
		authGRPCAddr = "auth_service:9090"
	}

	disableLLM := os.Getenv("DISABLE_LLM") == "1" || os.Getenv("DISABLE_LLM") == "true"
	dsTimeout := 60 * time.Second
	if v := os.Getenv("DEEPSEEK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			dsTimeout = d
		}
	}

	var llmClient llm.AIClient
	if !disableLLM && dsToken != "" {
		llmClient = *llm.NewAIClient(llm.AIConfig{
			Token:      dsToken,
			HTTPClient: &http.Client{Timeout: dsTimeout},
		})
	} else {
		log.Printf("llm disabled: disable=%v token=%v", disableLLM, dsToken != "")
	}

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

	var llmPtr usecase.LLMClient
	if !disableLLM && dsToken != "" {
		llmPtr = &llmClient
	}

	analyzer := usecase.NewAnalyzer(llmPtr, repo, cacheTTL)
	if repo != nil {
		startDailyAnalysisScheduler(analyzer, repo)
	}
	authConn, err := grpc.Dial(authGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("auth grpc dial: %v", err)
	}
	defer authConn.Close()

	authClient := authpb.NewAuthServiceClient(authConn)
	analyzeHandler := handler.NewGRPCAnalyzeHandler(analyzer, authClient)
	authMW := middleware.NewAuthGRPCMiddleware(authURL, nil)

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(authMW.Unary()),
	)
	nexusai.RegisterAnalyzerServiceServer(grpcServer, analyzeHandler)

	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	errCh := make(chan error, 1)
	go func() {
		lis, err := net.Listen("tcp", grpcAddr)
		if err != nil {
			errCh <- err
			return
		}
		log.Printf("grpc listening on %s", grpcAddr)
		errCh <- grpcServer.Serve(lis)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatal(err)
	case sig := <-sigCh:
		log.Printf("shutdown signal: %s", sig.String())
		if repo != nil {
			repo.Close()
		}
		grpcServer.GracefulStop()
	}
}

func startDailyAnalysisScheduler(analyzer *usecase.Analyzer, repo *repository.Repository) {
	go func() {
		for {
			now := time.Now()
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			time.Sleep(time.Until(next))

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			users, err := repo.ListUsersWithTrackPoints(ctx)
			if err == nil {
				for _, id := range users {
					tz, _ := repo.GetUserSettings(ctx, id)
					_ = analyzer.AnalyzeAllPeriods(ctx, id, tz)
				}
			}
			cancel()
		}
	}()
}
