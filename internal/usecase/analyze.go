package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"nexus/internal/domain/analytics"
	"nexus/internal/dto"
	"sort"
	"time"

	"github.com/gofiber/fiber/v3"
)

func (a *Analyzer) Analyze(c fiber.Ctx, req dto.AnalyzeRequest) (*dto.AnalyzeResponse, error) {
	ctx := context.Background()
	if c != nil {
		if reqCtx := c.Context(); reqCtx != nil {
			ctx = reqCtx
		}
	}
	cacheKey, err := buildCacheKey(req)
	if err == nil && a.repo != nil {
		resp, ok, err := a.repo.GetCachedResponse(ctx, cacheKey)
		if err == nil && ok {
			return resp, nil
		}
	}

	loc := time.UTC
	if req.UserTZ != "" {
		l, err := time.LoadLocation(req.UserTZ)
		if err == nil {
			loc = l
		}
	}

	pts := make([]dto.TrackPoint, 0, len(req.Points))
	for _, p := range req.Points {
		p.TS = p.TS.In(loc)
		pts = append(pts, p)
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].TS.Before(pts[j].TS) })

	energyByHour := analytics.ComputeEnergyByHour(pts)
	energyByWeekday := analytics.ComputeEnergyByWeekday(pts)

	model := analytics.ComputeProductivityModel(pts, energyByHour, req.Constraints)

	var risk dto.BurnoutRisk
	if len(pts) >= 10 {
		risk = analytics.ComputeBurnoutRisk(pts, model)
	} else {
		risk = dto.BurnoutRisk{
			Score:                 0,
			Level:                 "unknown",
			Reasons:               []string{"Недостаточно данных для прогноза выгорания (нужно хотя бы 10 точек)."},
			PredictionHorizonDays: 14,
		}
	}

	schedule := analytics.ComputeOptimalSchedule(energyByHour, pts)

	obsHours := analytics.ObservedHoursList(energyByHour)
	obsDays := analytics.ObservedWeekdaysList(energyByWeekday)

	llmText, err := a.llm.CallInsight(c, dto.HFPrompt{
		UserTZ:               req.UserTZ,
		EnergyByHour:         energyByHour,
		EnergyByWeekday:      energyByWeekday,
		ProductivityScore:    model.Score,
		BurnoutScore:         risk.Score,
		BurnoutLevel:         risk.Level,
		BurnoutReasons:       risk.Reasons,
		ProposedSchedule:     schedule,
		NumPoints:            len(pts),
		NumObservedHours:     len(energyByHour),
		NumObservedWeekdays:  len(energyByWeekday),
		ObservedHoursList:    obsHours,
		ObservedWeekdaysList: obsDays,
	})
	if err != nil {
		llmText = "LLM insight unavailable: " + err.Error()
	}

	resp := &dto.AnalyzeResponse{
		EnergyByHour:      energyByHour,
		EnergyByWeekday:   energyByWeekday,
		ProductivityModel: model,
		BurnoutRisk:       risk,
		OptimalSchedule:   schedule,
		LLMInsight:        llmText,
	}

	a.storeResult(ctx, cacheKey, req, *resp)

	return resp, nil
}

func buildCacheKey(req dto.AnalyzeRequest) (string, error) {
	normalized := req
	if len(req.Points) > 0 {
		normalized.Points = append([]dto.TrackPoint(nil), req.Points...)
	}
	if len(normalized.Points) > 1 {
		sort.Slice(normalized.Points, func(i, j int) bool {
			return normalized.Points[i].TS.Before(normalized.Points[j].TS)
		})
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func (a *Analyzer) storeResult(ctx context.Context, key string, req dto.AnalyzeRequest, resp dto.AnalyzeResponse) {
	if a.repo == nil || key == "" {
		return
	}
	_ = a.repo.CacheResponse(ctx, key, resp, a.cacheTTL)
	_ = a.repo.SaveAnalysis(ctx, key, req, resp)
}
