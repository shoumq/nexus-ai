package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"nexus/internal/domain/analytics"
	"nexus/internal/dto"
	"time"
)

func (a *Analyzer) Analyze(ctx context.Context, req dto.AnalyzeRequest) (*dto.AnalyzeResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if req.UserID <= 0 {
		return nil, errors.New("user id is required")
	}

	loc := time.UTC
	if req.UserTZ != "" {
		l, err := time.LoadLocation(req.UserTZ)
		if err == nil {
			loc = l
		}
	}

	cacheKey, err := buildCacheKey(req)
	if err == nil && a.repo != nil {
		resp, ok, err := a.repo.GetCachedResponse(ctx, cacheKey)
		if err == nil && ok {
			return resp, nil
		}
	}

	start, end := periodRange(req.Period, time.Now().In(loc))
	if a.repo == nil {
		return nil, errors.New("repository not configured")
	}
	pts, err := a.repo.GetTrackPoints(ctx, req.UserID, start.UTC(), end.UTC())
	if err != nil {
		return nil, err
	}
	if len(pts) < 2 {
		return nil, errors.New("need at least 2 points for stable analytics")
	}
	for i := range pts {
		pts[i].TS = pts[i].TS.In(loc)
	}

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

	llmText, err := a.llm.CallInsight(ctx, dto.HFPrompt{
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

func (a *Analyzer) Track(ctx context.Context, req dto.TrackRequest) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return 0, errors.New("repository not configured")
	}
	if req.UserID <= 0 {
		return 0, errors.New("user id is required")
	}
	if len(req.Points) == 0 {
		return 0, nil
	}
	return a.repo.SaveTrackPoints(ctx, req.UserID, req.Points)
}

func buildCacheKey(req dto.AnalyzeRequest) (string, error) {
	normalized := req
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

func periodRange(period dto.Period, now time.Time) (time.Time, time.Time) {
	switch period {
	case dto.PeriodDay:
		return now.AddDate(0, 0, -1), now
	case dto.PeriodWeek:
		return now.AddDate(0, 0, -7), now
	case dto.PeriodMonth:
		return now.AddDate(0, -1, 0), now
	case dto.PeriodAll, dto.PeriodUnspecified:
		return time.Time{}, now
	default:
		return time.Time{}, now
	}
}
