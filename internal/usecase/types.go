package usecase

import (
	"context"
	"nexus/internal/dto"
	"time"

	"github.com/gofiber/fiber/v3"
)

type LLMClient interface {
	CallInsight(c fiber.Ctx, p dto.HFPrompt) (string, error)
}

type AnalysisRepository interface {
	GetCachedResponse(ctx context.Context, key string) (*dto.AnalyzeResponse, bool, error)
	CacheResponse(ctx context.Context, key string, resp dto.AnalyzeResponse, ttl time.Duration) error
	SaveAnalysis(ctx context.Context, key string, req dto.AnalyzeRequest, resp dto.AnalyzeResponse) error
}

type Analyzer struct {
	llm      LLMClient
	repo     AnalysisRepository
	cacheTTL time.Duration
}

func NewAnalyzer(llm LLMClient, repo AnalysisRepository, cacheTTL time.Duration) *Analyzer {
	return &Analyzer{llm: llm, repo: repo, cacheTTL: cacheTTL}
}
