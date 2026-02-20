package handler

import (
	"context"
	"errors"
	"nexus/internal/dto"
	"nexus/internal/usecase"
	"nexus/proto/nexusai/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type GRPCAnalyzeHandler struct {
	nexusai.UnimplementedAnalyzerServiceServer
	analyzer *usecase.Analyzer
}

func NewGRPCAnalyzeHandler(analyzer *usecase.Analyzer) *GRPCAnalyzeHandler {
	return &GRPCAnalyzeHandler{analyzer: analyzer}
}

func (h *GRPCAnalyzeHandler) Analyze(ctx context.Context, req *nexusai.AnalyzeRequest) (*nexusai.AnalyzeResponse, error) {
	dtoReq, err := mapAnalyzeRequest(req)
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

func mapAnalyzeRequest(in *nexusai.AnalyzeRequest) (dto.AnalyzeRequest, error) {
	if in == nil {
		return dto.AnalyzeRequest{}, errors.New("empty request")
	}

	points := make([]dto.TrackPoint, 0, len(in.Points))
	for _, p := range in.Points {
		if p == nil || p.Ts == nil {
			return dto.AnalyzeRequest{}, errors.New("point timestamp is required")
		}
		points = append(points, dto.TrackPoint{
			TS:         p.Ts.AsTime(),
			SleepHours: p.SleepHours,
			Mood:       p.Mood,
			Activity:   p.Activity,
			Productive: p.Productive,
		})
	}

	var c dto.Constraints
	if in.Constraints != nil {
		c = dto.Constraints{
			WorkStartHour: int(in.Constraints.WorkStartHour),
			WorkEndHour:   int(in.Constraints.WorkEndHour),
		}
	}

	return dto.AnalyzeRequest{
		UserTZ:      in.UserTz,
		Points:      points,
		WeekStarts:  in.WeekStarts,
		Constraints: c,
	}, nil
}

func mapAnalyzeResponse(in *dto.AnalyzeResponse) (*nexusai.AnalyzeResponse, error) {
	if in == nil {
		return nil, errors.New("empty response")
	}

	energyByHour := make(map[int32]float64, len(in.EnergyByHour))
	for k, v := range in.EnergyByHour {
		energyByHour[int32(k)] = v
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
		EnergyByHour:      energyByHour,
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
