package handler

import "github.com/gofiber/fiber/v3"

func WithCORS() fiber.Handler {
	return func(c fiber.Ctx) error {
		origin := c.Get("Origin")
		if origin != "" {
			c.Set("Access-Control-Allow-Origin", origin)
			c.Set("Vary", "Origin")
			c.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			c.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		if c.Method() == fiber.MethodOptions {
			return c.SendStatus(fiber.StatusNoContent)
		}

		return c.Next()
	}
}
