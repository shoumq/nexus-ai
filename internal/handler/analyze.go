package handler

import (
	"context"
	"nexus/internal/dto"

	"github.com/gofiber/fiber/v3"
)

func (h *AnalyzeHandler) Handle(c fiber.Ctx) error {
	var req dto.AnalyzeRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "bad json: "+err.Error())
	}

	resp, err := h.Analyzer.Analyze(context.Background(), req)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "analyze error: "+err.Error())
	}

	c.Set(fiber.HeaderContentType, "application/json; charset=utf-8")
	return c.JSON(resp)
}
