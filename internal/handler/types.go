package handler

import (
	"nexus/internal/usecase"

	"github.com/gofiber/fiber/v3"
)

type AnalyzeHandler struct {
	Analyzer *usecase.Analyzer
}

func NewAnalyzeHandler(analyzer *usecase.Analyzer) *AnalyzeHandler {
	return &AnalyzeHandler{Analyzer: analyzer}
}

func (h *AnalyzeHandler) Handler() fiber.Handler {
	return h.Handle
}
