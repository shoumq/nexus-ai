package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"nexus/internal/dto"
)

func (a *Analyzer) GetMyProfile(ctx context.Context, userID int32) (dto.UserProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return dto.UserProfile{}, errors.New("repository not configured")
	}
	return a.repo.GetUserProfile(ctx, userID)
}

func (a *Analyzer) UpdateMyProfile(ctx context.Context, userID int32, emoji string, bgIndex int32) (dto.UserProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return dto.UserProfile{}, errors.New("repository not configured")
	}
	return a.repo.UpdateUserProfile(ctx, userID, emoji, bgIndex)
}

func (a *Analyzer) GetUserProfileForViewer(ctx context.Context, viewerID, targetID int32) (dto.UserProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return dto.UserProfile{}, errors.New("repository not configured")
	}
	return a.repo.GetUserProfileForViewer(ctx, viewerID, targetID)
}

func (a *Analyzer) GetUserLastAnalysesForViewer(ctx context.Context, viewerID, targetID int32) (map[string]dto.AnalyzeResponse, map[string]time.Time, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return nil, nil, errors.New("repository not configured")
	}
	if viewerID <= 0 || targetID <= 0 {
		return nil, nil, errors.New("invalid user id")
	}
	if viewerID != targetID {
		if _, err := a.repo.GetUserProfileForViewer(ctx, viewerID, targetID); err != nil {
			return nil, nil, err
		}
	}
	return a.repo.GetLastAnalyses(ctx, targetID)
}

func (a *Analyzer) SearchUsers(ctx context.Context, userID int32, query string) ([]dto.UserProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return nil, errors.New("repository not configured")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []dto.UserProfile{}, nil
	}
	return a.repo.SearchUsers(ctx, query, userID, 20)
}

func (a *Analyzer) ListFriends(ctx context.Context, userID int32) ([]dto.UserProfile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return nil, errors.New("repository not configured")
	}
	return a.repo.ListFriends(ctx, userID)
}

func (a *Analyzer) ListFriendRequests(ctx context.Context, userID int32, status string) ([]dto.FriendRequest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return nil, errors.New("repository not configured")
	}
	return a.repo.ListFriendRequests(ctx, userID, status)
}

func (a *Analyzer) SendFriendRequest(ctx context.Context, fromUserID, toUserID int32) (dto.FriendRequest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return dto.FriendRequest{}, errors.New("repository not configured")
	}
	return a.repo.CreateFriendRequest(ctx, fromUserID, toUserID)
}

func (a *Analyzer) RespondFriendRequest(ctx context.Context, userID int32, requestID int64, action string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.repo == nil {
		return errors.New("repository not configured")
	}
	return a.repo.RespondFriendRequest(ctx, userID, requestID, action)
}
