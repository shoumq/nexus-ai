package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

type AuthMiddleware struct {
	authURL string
	client  *http.Client
}

func NewAuthMiddleware(authURL string, client *http.Client) *AuthMiddleware {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	return &AuthMiddleware{
		authURL: strings.TrimSpace(authURL),
		client:  client,
	}
}

func (m *AuthMiddleware) Handler() fiber.Handler {
	return func(c fiber.Ctx) error {
		switch c.Path() {
		case "/health", "/ready", "/metrics":
			return c.Next()
		}

		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing Authorization header")
		}

		if m.authURL == "" {
			// Fallback: header-only auth check when auth service is not configured.
			return c.Next()
		}

		req, err := http.NewRequestWithContext(c.Context(), http.MethodPost, m.authURL, nil)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "auth request build failed")
		}
		req.Header.Set("Authorization", authHeader)

		if rid := c.Get("X-Request-Id"); rid != "" {
			req.Header.Set("X-Request-Id", rid)
		}

		resp, err := m.client.Do(req)
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, "auth service unavailable")
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
		}

		return c.Next()
	}
}
