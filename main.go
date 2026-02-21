package main

import (
	"context"
	"log"
	"net"
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

	hfToken := os.Getenv("HF_TOKEN")
	authURL := os.Getenv("AUTH_URL")
	authGRPCAddr := os.Getenv("AUTH_GRPC_ADDR")
	if authGRPCAddr == "" {
		authGRPCAddr = "auth_service:9090"
	}
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
