package handler

import (
	authpb "auth_service/proto"
	"context"
	"errors"
	"nexus/internal/dto"
	"nexus/internal/usecase"
	"nexus/proto/nexusai/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type GRPCAnalyzeHandler struct {
	nexusai.UnimplementedAnalyzerServiceServer
	analyzer   *usecase.Analyzer
	authClient authpb.AuthServiceClient
}

func NewGRPCAnalyzeHandler(analyzer *usecase.Analyzer, authClient authpb.AuthServiceClient) *GRPCAnalyzeHandler {
	return &GRPCAnalyzeHandler{analyzer: analyzer, authClient: authClient}
}

func (h *GRPCAnalyzeHandler) Track(ctx context.Context, req *nexusai.TrackRequest) (*nexusai.TrackResponse, error) {
	userID, err := h.userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	dtoReq, err := mapTrackRequest(req, userID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	stored, err := h.analyzer.Track(ctx, dtoReq)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &nexusai.TrackResponse{Stored: int32(stored)}, nil
}

func (h *GRPCAnalyzeHandler) Analyze(ctx context.Context, req *nexusai.AnalyzeRequest) (*nexusai.AnalyzeResponse, error) {
	userID, err := h.userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	dtoReq, err := mapAnalyzeRequest(req, userID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	resp, err := h.analyzer.Analyze(ctx, dtoReq)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	out, err := mapAnalyzeResponse(resp)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return out, nil
}

func (h *GRPCAnalyzeHandler) GetTodayTrack(ctx context.Context, req *nexusai.TodayTrackRequest) (*nexusai.TodayTrackResponse, error) {
	userID, err := h.userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	userTZ := ""
	if req != nil {
		userTZ = req.GetUserTz()
	}
	p, ok, err := h.analyzer.GetTodayTrack(ctx, userID, userTZ)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !ok {
		return &nexusai.TodayTrackResponse{Exists: false}, nil
	}
	return &nexusai.TodayTrackResponse{
		Exists: true,
		Point: &nexusai.TrackPoint{
			Ts:            timestamppb.New(p.TS),
			SleepHours:    p.SleepHours,
			Mood:          p.Mood,
			Activity:      p.Activity,
			Productive:    p.Productive,
			Stress:        p.Stress,
			Energy:        p.Energy,
			Concentration: p.Concentration,
			SleepQuality:  p.SleepQuality,
			Caffeine:      p.Caffeine,
			Alcohol:       p.Alcohol,
			Workout:       p.Workout,
			LlmText:       p.LLMText,
		},
	}, nil
}

func (h *GRPCAnalyzeHandler) GetLastAnalyses(ctx context.Context, _ *nexusai.LastAnalysesRequest) (*nexusai.LastAnalysesResponse, error) {
	userID, err := h.userIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	m, meta, err := h.analyzer.GetLastAnalyses(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := &nexusai.LastAnalysesResponse{}
	for period, resp := range m {
		updatedAt := meta[period]
		pb, err := mapAnalyzeResponse(&resp)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		out.Entries = append(out.Entries, &nexusai.LastAnalysisEntry{
			Period:    period,
			Response:  pb,
			UpdatedAt: timestamppb.New(updatedAt),
		})
	}
	return out, nil
}

func mapTrackRequest(in *nexusai.TrackRequest, userID int32) (dto.TrackRequest, error) {
	if in == nil {
		return dto.TrackRequest{}, errors.New("empty request")
	}

	points := make([]dto.TrackPoint, 0, len(in.Points))
	for _, p := range in.Points {
		if p == nil || p.Ts == nil {
			return dto.TrackRequest{}, errors.New("point timestamp is required")
		}
		points = append(points, dto.TrackPoint{
			TS:            p.Ts.AsTime(),
			SleepHours:    p.SleepHours,
			Mood:          p.Mood,
			Activity:      p.Activity,
			Productive:    p.Productive,
			Stress:        p.Stress,
			Energy:        p.Energy,
			Concentration: p.Concentration,
			SleepQuality:  p.SleepQuality,
			Caffeine:      p.Caffeine,
			Alcohol:       p.Alcohol,
			Workout:       p.Workout,
			LLMText:       p.LlmText,
		})
	}

	return dto.TrackRequest{
		UserID: userID,
		UserTZ: in.UserTz,
		Points: points,
	}, nil
}

func mapAnalyzeRequest(in *nexusai.AnalyzeRequest, userID int32) (dto.AnalyzeRequest, error) {
	if in == nil {
		return dto.AnalyzeRequest{}, errors.New("empty request")
	}

	var c dto.Constraints
	if in.Constraints != nil {
		c = dto.Constraints{
			WorkStartHour: int(in.Constraints.WorkStartHour),
			WorkEndHour:   int(in.Constraints.WorkEndHour),
		}
	}

	return dto.AnalyzeRequest{
		UserID:      userID,
		UserTZ:      in.UserTz,
		WeekStarts:  in.WeekStarts,
		Constraints: c,
		Period:      mapPeriod(in.Period),
	}, nil
}

func mapAnalyzeResponse(in *dto.AnalyzeResponse) (*nexusai.AnalyzeResponse, error) {
	if in == nil {
		return nil, errors.New("empty response")
	}

	energyByWeekday := make(map[string]float64, len(in.EnergyByWeekday))
	for k, v := range in.EnergyByWeekday {
		energyByWeekday[k] = v
	}

	model := &nexusai.ProductivityModel{
		Score: in.ProductivityModel.Score,
		Weights: func() map[string]float64 {
			out := make(map[string]float64, len(in.ProductivityModel.Weights))
			for k, v := range in.ProductivityModel.Weights {
				out[k] = v
			}
			return out
		}(),
	}

	burnout := &nexusai.BurnoutRisk{
		Score:                 in.BurnoutRisk.Score,
		Level:                 in.BurnoutRisk.Level,
		Reasons:               append([]string(nil), in.BurnoutRisk.Reasons...),
		PredictionHorizonDays: int32(in.BurnoutRisk.PredictionHorizonDays),
	}

	schedule := &nexusai.OptimalSchedule{
		SuggestedSleepWindow: in.OptimalSchedule.SuggestedSleepWindow,
		BestFocusHours:       append([]string(nil), in.OptimalSchedule.BestFocusHours...),
		BestLightTasksHours:  append([]string(nil), in.OptimalSchedule.BestLightTasksHours...),
		RecoveryTips:         append([]string(nil), in.OptimalSchedule.RecoveryTips...),
	}

	out := &nexusai.AnalyzeResponse{
		EnergyByWeekday:   energyByWeekday,
		ProductivityModel: model,
		BurnoutRisk:       burnout,
		OptimalSchedule:   schedule,
		LlmInsight:        in.LLMInsight,
	}

	if in.Debug != nil {
		s, err := structpb.NewStruct(in.Debug)
		if err != nil {
			return nil, err
		}
		out.Debug = s
	}

	return out, nil
}

func (h *GRPCAnalyzeHandler) userIDFromContext(ctx context.Context) (int32, error) {
	if h.authClient == nil {
		return 0, status.Error(codes.Internal, "auth client not configured")
	}
	authHeader := authFromMetadata(ctx)
	if authHeader == "" {
		return 0, status.Error(codes.Unauthenticated, "missing authorization")
	}
	outCtx := metadata.AppendToOutgoingContext(ctx, "authorization", authHeader)
	resp, err := h.authClient.Me(outCtx, &authpb.MeRequest{})
	if err != nil {
		return 0, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if resp == nil || resp.Id == 0 {
		return 0, status.Error(codes.Unauthenticated, "unauthorized")
	}
	return resp.Id, nil
}

func authFromMetadata(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	if md == nil {
		return ""
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func mapPeriod(p nexusai.Period) dto.Period {
	switch p {
	case nexusai.Period_PERIOD_DAY:
		return dto.PeriodDay
	case nexusai.Period_PERIOD_WEEK:
		return dto.PeriodWeek
	case nexusai.Period_PERIOD_MONTH:
		return dto.PeriodMonth
	case nexusai.Period_PERIOD_ALL:
		return dto.PeriodAll
	default:
		return dto.PeriodUnspecified
	}
}
