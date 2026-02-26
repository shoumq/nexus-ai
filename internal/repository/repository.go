package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"nexus/internal/dto"

	"github.com/jackc/pgx/v5"
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

func (r *Repository) SaveTrackPoints(ctx context.Context, userID int32, pts []dto.TrackPoint) (int, error) {
	if r.pg == nil {
		return 0, errors.New("repository: postgres not configured")
	}
	if userID <= 0 || len(pts) == 0 {
		return 0, nil
	}

	batch := &pgx.Batch{}
	for _, p := range pts {
		bucket := p.TS.Unix() / 300
		batch.Queue(`
			insert into track_points (
				user_id, ts, sleep_hours, mood, activity, productive,
				stress, energy, concentration, sleep_quality,
				caffeine, alcohol, workout, llm_text, time_bucket_5m
			)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			on conflict (user_id, time_bucket_5m) do nothing
		`, userID, p.TS, p.SleepHours, p.Mood, p.Activity, p.Productive,
			p.Stress, p.Energy, p.Concentration, p.SleepQuality,
			p.Caffeine, p.Alcohol, p.Workout, p.LLMText, bucket)
	}

	br := r.pg.SendBatch(ctx, batch)
	defer br.Close()

	inserted := 0
	for range pts {
		ct, err := br.Exec()
		if err != nil {
			return inserted, err
		}
		inserted += int(ct.RowsAffected())
	}
	return inserted, nil
}

func (r *Repository) GetTrackPoints(ctx context.Context, userID int32, from, to time.Time) ([]dto.TrackPoint, error) {
	if r.pg == nil {
		return nil, errors.New("repository: postgres not configured")
	}
	if userID <= 0 {
		return nil, errors.New("repository: invalid user id")
	}

	rows, err := r.pg.Query(ctx, `
		select ts, sleep_hours, mood, activity, productive,
		       stress, energy, concentration, sleep_quality,
		       caffeine, alcohol, workout, llm_text
		from track_points
		where user_id = $1 and ts >= $2 and ts <= $3
		order by ts asc
	`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []dto.TrackPoint
	for rows.Next() {
		var p dto.TrackPoint
		if err := rows.Scan(
			&p.TS, &p.SleepHours, &p.Mood, &p.Activity, &p.Productive,
			&p.Stress, &p.Energy, &p.Concentration, &p.SleepQuality,
			&p.Caffeine, &p.Alcohol, &p.Workout, &p.LLMText,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) GetTrackPointForDay(ctx context.Context, userID int32, from, to time.Time) (dto.TrackPoint, bool, error) {
	if r.pg == nil {
		return dto.TrackPoint{}, false, errors.New("repository: postgres not configured")
	}
	if userID <= 0 {
		return dto.TrackPoint{}, false, errors.New("repository: invalid user id")
	}
	var p dto.TrackPoint
	err := r.pg.QueryRow(ctx, `
		select ts, sleep_hours, mood, activity, productive,
		       stress, energy, concentration, sleep_quality,
		       caffeine, alcohol, workout, llm_text
		from track_points
		where user_id = $1 and ts >= $2 and ts < $3
		order by ts desc
		limit 1
	`, userID, from, to).Scan(
		&p.TS, &p.SleepHours, &p.Mood, &p.Activity, &p.Productive,
		&p.Stress, &p.Energy, &p.Concentration, &p.SleepQuality,
		&p.Caffeine, &p.Alcohol, &p.Workout, &p.LLMText,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dto.TrackPoint{}, false, nil
		}
		return dto.TrackPoint{}, false, err
	}
	return p, true, nil
}

func (r *Repository) UpsertTrackPointForDay(ctx context.Context, userID int32, p dto.TrackPoint, from, to time.Time) (bool, error) {
	if r.pg == nil {
		return false, errors.New("repository: postgres not configured")
	}
	if userID <= 0 {
		return false, errors.New("repository: invalid user id")
	}
	var id int64
	err := r.pg.QueryRow(ctx, `
		select id from track_points
		where user_id = $1 and ts >= $2 and ts < $3
		order by ts desc
		limit 1
	`, userID, from, to).Scan(&id)
	bucket := p.TS.Unix() / 300
	if err == nil {
		_, err = r.pg.Exec(ctx, `
			update track_points
			set ts = $2,
			    sleep_hours = $3,
			    mood = $4,
			    activity = $5,
			    productive = $6,
			    stress = $7,
			    energy = $8,
			    concentration = $9,
			    sleep_quality = $10,
			    caffeine = $11,
			    alcohol = $12,
			    workout = $13,
			    llm_text = $14,
			    time_bucket_5m = $15
			where id = $1
		`, id, p.TS, p.SleepHours, p.Mood, p.Activity, p.Productive,
			p.Stress, p.Energy, p.Concentration, p.SleepQuality,
			p.Caffeine, p.Alcohol, p.Workout, p.LLMText, bucket)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	_, err = r.pg.Exec(ctx, `
		insert into track_points (
			user_id, ts, sleep_hours, mood, activity, productive,
			stress, energy, concentration, sleep_quality,
			caffeine, alcohol, workout, llm_text, time_bucket_5m
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`, userID, p.TS, p.SleepHours, p.Mood, p.Activity, p.Productive,
		p.Stress, p.Energy, p.Concentration, p.SleepQuality,
		p.Caffeine, p.Alcohol, p.Workout, p.LLMText, bucket)
	if err != nil {
		return false, err
	}
	return false, nil
}

func (r *Repository) ListUsersWithTrackPoints(ctx context.Context) ([]int32, error) {
	if r.pg == nil {
		return nil, errors.New("repository: postgres not configured")
	}
	rows, err := r.pg.Query(ctx, `select distinct user_id from track_points`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) UpsertLastAnalysis(ctx context.Context, userID int32, period string, resp dto.AnalyzeResponse) error {
	if r.pg == nil {
		return errors.New("repository: postgres not configured")
	}
	if userID <= 0 || period == "" {
		return errors.New("repository: invalid input")
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = r.pg.Exec(ctx, `
		insert into last_analyses (user_id, period, response, updated_at)
		values ($1, $2, $3, now())
		on conflict (user_id, period) do update
		set response = excluded.response,
		    updated_at = excluded.updated_at
	`, userID, period, b)
	return err
}

func (r *Repository) GetLastAnalyses(ctx context.Context, userID int32) (map[string]dto.AnalyzeResponse, map[string]time.Time, error) {
	if r.pg == nil {
		return nil, nil, errors.New("repository: postgres not configured")
	}
	if userID <= 0 {
		return nil, nil, errors.New("repository: invalid user id")
	}
	rows, err := r.pg.Query(ctx, `
		select period, response, updated_at
		from last_analyses
		where user_id = $1
	`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := make(map[string]dto.AnalyzeResponse)
	meta := make(map[string]time.Time)
	for rows.Next() {
		var period string
		var b []byte
		var ts time.Time
		if err := rows.Scan(&period, &b, &ts); err != nil {
			return nil, nil, err
		}
		var resp dto.AnalyzeResponse
		if err := json.Unmarshal(b, &resp); err != nil {
			return nil, nil, err
		}
		out[period] = resp
		meta[period] = ts
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, meta, nil
}

func (r *Repository) UpsertUserSettings(ctx context.Context, userID int32, userTZ string) error {
	if r.pg == nil {
		return errors.New("repository: postgres not configured")
	}
	if userID <= 0 {
		return errors.New("repository: invalid user id")
	}
	if userTZ == "" {
		userTZ = "UTC"
	}
	_, err := r.pg.Exec(ctx, `
		insert into user_settings (user_id, user_tz, updated_at)
		values ($1, $2, now())
		on conflict (user_id) do update
		set user_tz = excluded.user_tz,
		    updated_at = excluded.updated_at
	`, userID, userTZ)
	return err
}

func (r *Repository) GetUserSettings(ctx context.Context, userID int32) (string, error) {
	if r.pg == nil {
		return "", errors.New("repository: postgres not configured")
	}
	if userID <= 0 {
		return "", errors.New("repository: invalid user id")
	}
	var tz string
	err := r.pg.QueryRow(ctx, `select user_tz from user_settings where user_id = $1`, userID).Scan(&tz)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "UTC", nil
		}
		return "", err
	}
	if tz == "" {
		tz = "UTC"
	}
	return tz, nil
}

func cacheKey(key string) string {
	return "analysis:cache:" + key
}
