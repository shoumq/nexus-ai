package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type AuthGRPCMiddleware struct {
	authURL string
	client  *http.Client
}

func NewAuthGRPCMiddleware(authURL string, client *http.Client) *AuthGRPCMiddleware {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	return &AuthGRPCMiddleware{
		authURL: strings.TrimSpace(authURL),
		client:  client,
	}
}

func (m *AuthGRPCMiddleware) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if isHealthMethod(info.FullMethod) {
			return handler(ctx, req)
		}

		md, _ := metadata.FromIncomingContext(ctx)
		authHeader := firstMeta(md, "authorization")
		if authHeader == "" {
			return nil, status.Error(codes.Unauthenticated, "missing authorization")
		}

		if m.authURL == "" {
			return handler(ctx, req)
		}

		reqHTTP, err := http.NewRequestWithContext(ctx, http.MethodPost, m.authURL, nil)
		if err != nil {
			return nil, status.Error(codes.Internal, "auth request build failed")
		}
		reqHTTP.Header.Set("Authorization", authHeader)
		if rid := firstMeta(md, "x-request-id"); rid != "" {
			reqHTTP.Header.Set("X-Request-Id", rid)
		}

		resp, err := m.client.Do(reqHTTP)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "auth service unavailable")
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, status.Error(codes.Unauthenticated, "unauthorized")
		}

		return handler(ctx, req)
	}
}

func isHealthMethod(fullMethod string) bool {
	return strings.HasPrefix(fullMethod, "/grpc.health.v1.Health/")
}

func firstMeta(md metadata.MD, key string) string {
	if md == nil {
		return ""
	}
	vals := md.Get(key)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}
