package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"nexus/internal/dto"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Repository struct {
	pg    *pgxpool.Pool
	redis *redis.Client
}

func NewRepository(ctx context.Context, cfg Config) (*Repository, error) {
	repo := &Repository{}

	if cfg.PostgresURL != "" {
		pg, err := pgxpool.New(ctx, cfg.PostgresURL)
		if err != nil {
			return nil, err
		}
		if err := pg.Ping(ctx); err != nil {
			pg.Close()
			return nil, err
		}
		repo.pg = pg
	}

	if cfg.RedisAddr != "" {
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		if err := rdb.Ping(ctx).Err(); err != nil {
			return nil, err
		}
		repo.redis = rdb
	}

	if repo.pg == nil && repo.redis == nil {
		return nil, errors.New("repository: no postgres or redis configured")
	}

	return repo, nil
}

func (r *Repository) Close() {
	if r.pg != nil {
		r.pg.Close()
	}
	if r.redis != nil {
		_ = r.redis.Close()
	}
}

func (r *Repository) GetCachedResponse(ctx context.Context, key string) (*dto.AnalyzeResponse, bool, error) {
	if r.redis == nil || key == "" {
		return nil, false, nil
	}
	raw, err := r.redis.Get(ctx, cacheKey(key)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var resp dto.AnalyzeResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false, err
	}
	return &resp, true, nil
}

func (r *Repository) CacheResponse(ctx context.Context, key string, resp dto.AnalyzeResponse, ttl time.Duration) error {
	if r.redis == nil || key == "" || ttl <= 0 {
		return nil
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return r.redis.Set(ctx, cacheKey(key), raw, ttl).Err()
}

func (r *Repository) SaveAnalysis(ctx context.Context, key string, req dto.AnalyzeRequest, resp dto.AnalyzeResponse) error {
	if r.pg == nil || key == "" {
		return nil
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return err
	}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	_, err = r.pg.Exec(ctx, `
		insert into analyses (id, request, response, created_at)
		values ($1, $2, $3, now())
		on conflict (id) do update
		set request = excluded.request,
		    response = excluded.response,
		    created_at = excluded.created_at
	`, key, reqJSON, respJSON)
	return err
}

func cacheKey(key string) string {
	return "analysis:cache:" + key
}
