package usecase

import (
	"context"
	"nexus/internal/dto"
	"time"
)

type LLMClient interface {
	CallInsight(ctx context.Context, p dto.AIPrompt) (string, error)
}

type AnalysisRepository interface {
	GetCachedResponse(ctx context.Context, key string) (*dto.AnalyzeResponse, bool, error)
	CacheResponse(ctx context.Context, key string, resp dto.AnalyzeResponse, ttl time.Duration) error
	SaveAnalysis(ctx context.Context, key string, req dto.AnalyzeRequest, resp dto.AnalyzeResponse) error
	SaveTrackPoints(ctx context.Context, userID int32, pts []dto.TrackPoint) (int, error)
	GetTrackPoints(ctx context.Context, userID int32, from, to time.Time) ([]dto.TrackPoint, error)
	GetTrackPointForDay(ctx context.Context, userID int32, from, to time.Time) (dto.TrackPoint, bool, error)
	UpsertTrackPointForDay(ctx context.Context, userID int32, p dto.TrackPoint, from, to time.Time) (bool, error)
	ListUsersWithTrackPoints(ctx context.Context) ([]int32, error)
	SetAnalysisStatusForDay(ctx context.Context, userID int32, from, to time.Time, status, errText string) error
	UpsertLastAnalysis(ctx context.Context, userID int32, period string, resp dto.AnalyzeResponse) error
	GetLastAnalyses(ctx context.Context, userID int32) (map[string]dto.AnalyzeResponse, map[string]time.Time, error)
	UpsertUserSettings(ctx context.Context, userID int32, userTZ string) error
	GetUserSettings(ctx context.Context, userID int32) (string, error)
	GetUserProfile(ctx context.Context, userID int32) (dto.UserProfile, error)
	UpdateUserProfile(ctx context.Context, userID int32, emoji string, bgIndex int32) (dto.UserProfile, error)
	GetUserProfileForViewer(ctx context.Context, viewerID, targetID int32) (dto.UserProfile, error)
	SearchUsers(ctx context.Context, query string, excludeUserID int32, limit int) ([]dto.UserProfile, error)
	ListFriends(ctx context.Context, userID int32) ([]dto.UserProfile, error)
	CreateFriendRequest(ctx context.Context, fromUserID, toUserID int32) (dto.FriendRequest, error)
	ListFriendRequests(ctx context.Context, userID int32, status string) ([]dto.FriendRequest, error)
	RespondFriendRequest(ctx context.Context, userID int32, requestID int64, action string) error
}

type Analyzer struct {
	llm      LLMClient
	repo     AnalysisRepository
	cacheTTL time.Duration
}

func NewAnalyzer(llm LLMClient, repo AnalysisRepository, cacheTTL time.Duration) *Analyzer {
	return &Analyzer{llm: llm, repo: repo, cacheTTL: cacheTTL}
}
