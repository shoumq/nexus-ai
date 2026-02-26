package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"nexus/internal/domain/analytics"
	"nexus/internal/dto"
	"strings"
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
	if err == nil && a.repo != nil && a.llm == nil {
		resp, ok, err := a.repo.GetCachedResponse(ctx, cacheKey)
		if err == nil && ok && resp != nil {
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

	energyByWeekday := analytics.ComputeEnergyByWeekday(pts)

	model := analytics.ComputeProductivityModel(pts)

	var risk dto.BurnoutRisk
	if len(pts) >= 5 {
		risk = analytics.ComputeBurnoutRisk(pts, model)
	} else {
		risk = dto.BurnoutRisk{
			Score:                 0,
			Level:                 "недостаточно данных",
			Reasons:               []string{"Недостаточно данных для прогноза выгорания (нужно хотя бы 5 точек)."},
			PredictionHorizonDays: 14,
		}
	}

	obsDays := analytics.ObservedWeekdaysList(energyByWeekday)
	userNotes := buildUserNotes(pts, 1200)

	llmText := "LLM disabled"
	if a.llm != nil {
		llmText, err = a.llm.CallInsight(ctx, dto.AIPrompt{
			UserTZ:               req.UserTZ,
			EnergyByWeekday:      energyByWeekday,
			ProductivityScore:    model.Score,
			BurnoutScore:         risk.Score,
			BurnoutLevel:         risk.Level,
			BurnoutReasons:       risk.Reasons,
			NumPoints:            len(pts),
			NumObservedWeekdays:  len(energyByWeekday),
			ObservedWeekdaysList: obsDays,
			UserNotes:            userNotes,
		})
		if err != nil {
			llmText = "LLM insight unavailable: " + err.Error()
		}
	}

	debug := map[string]any{}
	avgSleep := analytics.AvgSleepDays(pts, 14)
	if avgSleep > 0 {
		debug["avg_sleep_hours"] = avgSleep
	}
	sleepDelta := analytics.SleepDeltaDays(pts, 7)
	if sleepDelta != 0 {
		debug["avg_sleep_delta"] = sleepDelta
	}

	resp := &dto.AnalyzeResponse{
		EnergyByWeekday:   energyByWeekday,
		ProductivityModel: model,
		BurnoutRisk:       risk,
		OptimalSchedule:   dto.OptimalSchedule{},
		LLMInsight:        llmText,
		Debug:             debug,
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
	loc := time.UTC
	if req.UserTZ != "" {
		if l, err := time.LoadLocation(req.UserTZ); err == nil {
			loc = l
		}
	}
	p := req.Points[0]
	ts := p.TS.In(loc)
	start := time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	updated, err := a.repo.UpsertTrackPointForDay(ctx, req.UserID, p, start.UTC(), end.UTC())
	if err != nil {
		return 0, err
	}
	_ = a.repo.UpsertUserSettings(ctx, req.UserID, req.UserTZ)

	_ = a.runAnalysesForUser(ctx, req.UserID, req.UserTZ)

	if updated {
		return 0, nil
	}
	return 1, nil
}

func (a *Analyzer) runAnalysesForUser(ctx context.Context, userID int32, userTZ string) error {
	if a.repo == nil || userID <= 0 {
		return nil
	}
	if userTZ == "" {
		userTZ = "UTC"
	}
	periods := []dto.Period{dto.PeriodDay, dto.PeriodWeek, dto.PeriodMonth, dto.PeriodAll}
	c := dto.Constraints{WorkStartHour: 9, WorkEndHour: 18}
	for _, p := range periods {
		_, _ = a.Analyze(ctx, dto.AnalyzeRequest{
			UserID:      userID,
			UserTZ:      userTZ,
			WeekStarts:  "monday",
			Constraints: c,
			Period:      p,
		})
	}
	return nil
}

func (a *Analyzer) AnalyzeAllPeriods(ctx context.Context, userID int32, userTZ string) error {
	return a.runAnalysesForUser(ctx, userID, userTZ)
}

func (a *Analyzer) GetTodayTrack(ctx context.Context, userID int32, userTZ string) (dto.TrackPoint, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return dto.TrackPoint{}, false, errors.New("repository not configured")
	}
	if userID <= 0 {
		return dto.TrackPoint{}, false, errors.New("user id is required")
	}
	loc := time.UTC
	if userTZ != "" {
		if l, err := time.LoadLocation(userTZ); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	return a.repo.GetTrackPointForDay(ctx, userID, start.UTC(), end.UTC())
}

func (a *Analyzer) GetLastAnalyses(ctx context.Context, userID int32) (map[string]dto.AnalyzeResponse, map[string]time.Time, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return nil, nil, errors.New("repository not configured")
	}
	if userID <= 0 {
		return nil, nil, errors.New("user id is required")
	}
	return a.repo.GetLastAnalyses(ctx, userID)
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
	cacheResp := resp
	cacheResp.LLMInsight = ""
	_ = a.repo.CacheResponse(ctx, key, cacheResp, a.cacheTTL)
	_ = a.repo.SaveAnalysis(ctx, key, req, resp)
	if req.UserID > 0 {
		period := string(req.Period)
		if period == "" {
			period = "all"
		}
		_ = a.repo.UpsertLastAnalysis(ctx, req.UserID, period, resp)
	}
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

func buildUserNotes(pts []dto.TrackPoint, maxLen int) string {
	if len(pts) == 0 || maxLen <= 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range pts {
		txt := strings.TrimSpace(p.LLMText)
		if txt == "" {
			continue
		}
		line := p.TS.Format("2006-01-02 15:04") + " — " + txt
		if b.Len() > 0 {
			line = "\n" + line
		}
		if b.Len()+len(line) > maxLen {
			remain := maxLen - b.Len()
			if remain > 0 {
				b.WriteString(line[:remain])
			}
			break
		}
		b.WriteString(line)
	}
	return b.String()
}
