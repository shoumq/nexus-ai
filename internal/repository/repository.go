package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
				user_id, ts, sleep_hours, sleep_start, sleep_end, mood, activity, productive,
				stress, energy, concentration, sleep_quality,
				caffeine, alcohol, workout, llm_text, time_bucket_5m
			)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
			on conflict (user_id, time_bucket_5m) do nothing
		`, userID, p.TS, p.SleepHours, p.SleepStart, p.SleepEnd, p.Mood, p.Activity, p.Productive,
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
		select ts, sleep_hours, sleep_start, sleep_end, mood, activity, productive,
		       stress, energy, concentration, sleep_quality,
		       caffeine, alcohol, workout, llm_text, analysis_status
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
			&p.TS, &p.SleepHours, &p.SleepStart, &p.SleepEnd, &p.Mood, &p.Activity, &p.Productive,
			&p.Stress, &p.Energy, &p.Concentration, &p.SleepQuality,
			&p.Caffeine, &p.Alcohol, &p.Workout, &p.LLMText, &p.AnalysisStatus,
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
		select ts, sleep_hours, sleep_start, sleep_end, mood, activity, productive,
		       stress, energy, concentration, sleep_quality,
		       caffeine, alcohol, workout, llm_text, analysis_status
		from track_points
		where user_id = $1 and ts >= $2 and ts < $3
		order by ts desc
		limit 1
	`, userID, from, to).Scan(
		&p.TS, &p.SleepHours, &p.SleepStart, &p.SleepEnd, &p.Mood, &p.Activity, &p.Productive,
		&p.Stress, &p.Energy, &p.Concentration, &p.SleepQuality,
		&p.Caffeine, &p.Alcohol, &p.Workout, &p.LLMText, &p.AnalysisStatus,
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
			    sleep_start = $4,
			    sleep_end = $5,
			    mood = $6,
			    activity = $7,
			    productive = $8,
			    stress = $9,
			    energy = $10,
			    concentration = $11,
			    sleep_quality = $12,
			    caffeine = $13,
			    alcohol = $14,
			    workout = $15,
			    llm_text = $16,
			    time_bucket_5m = $17,
			    analysis_status = 'pending',
			    analysis_updated_at = now(),
			    analysis_error = ''
			where id = $1
		`, id, p.TS, p.SleepHours, p.SleepStart, p.SleepEnd, p.Mood, p.Activity, p.Productive,
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
			user_id, ts, sleep_hours, sleep_start, sleep_end, mood, activity, productive,
			stress, energy, concentration, sleep_quality,
			caffeine, alcohol, workout, llm_text, time_bucket_5m,
			analysis_status, analysis_updated_at, analysis_error
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, 'pending', now(), '')
	`, userID, p.TS, p.SleepHours, p.SleepStart, p.SleepEnd, p.Mood, p.Activity, p.Productive,
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

func (r *Repository) SetAnalysisStatusForDay(ctx context.Context, userID int32, from, to time.Time, status, errText string) error {
	if r.pg == nil {
		return errors.New("repository: postgres not configured")
	}
	if userID <= 0 {
		return errors.New("repository: invalid user id")
	}
	if status == "" {
		return errors.New("repository: status is required")
	}
	_, err := r.pg.Exec(ctx, `
		update track_points
		set analysis_status = $1,
		    analysis_updated_at = now(),
		    analysis_error = $2
		where user_id = $3 and ts >= $4 and ts < $5
	`, status, errText, userID, from, to)
	return err
}

func (r *Repository) GetUserProfile(ctx context.Context, userID int32) (dto.UserProfile, error) {
	if r.pg == nil {
		return dto.UserProfile{}, errors.New("repository: postgres not configured")
	}
	if userID <= 0 {
		return dto.UserProfile{}, errors.New("repository: invalid user id")
	}
	var p dto.UserProfile
	err := r.pg.QueryRow(ctx, `
		select u.id, u.name, u.email,
		       coalesce(s.avatar_emoji, '') as emoji,
		       coalesce(s.avatar_bg, 0) as bg
		from users u
		left join user_settings s on s.user_id = u.id
		where u.id = $1
	`, userID).Scan(&p.UserID, &p.Name, &p.Email, &p.Emoji, &p.BgIndex)
	if err != nil {
		return dto.UserProfile{}, err
	}
	return p, nil
}

func (r *Repository) UpdateUserProfile(ctx context.Context, userID int32, emoji string, bgIndex int32) (dto.UserProfile, error) {
	if r.pg == nil {
		return dto.UserProfile{}, errors.New("repository: postgres not configured")
	}
	if userID <= 0 {
		return dto.UserProfile{}, errors.New("repository: invalid user id")
	}
	_, err := r.pg.Exec(ctx, `
		insert into user_settings (user_id, avatar_emoji, avatar_bg, updated_at)
		values ($1, $2, $3, now())
		on conflict (user_id) do update
		set avatar_emoji = excluded.avatar_emoji,
		    avatar_bg = excluded.avatar_bg,
		    updated_at = excluded.updated_at
	`, userID, emoji, bgIndex)
	if err != nil {
		return dto.UserProfile{}, err
	}
	return r.GetUserProfile(ctx, userID)
}

func (r *Repository) GetUserProfileForViewer(ctx context.Context, viewerID, targetID int32) (dto.UserProfile, error) {
	if r.pg == nil {
		return dto.UserProfile{}, errors.New("repository: postgres not configured")
	}
	if viewerID <= 0 || targetID <= 0 {
		return dto.UserProfile{}, errors.New("repository: invalid user id")
	}
	if viewerID == targetID {
		return r.GetUserProfile(ctx, targetID)
	}
	var isFriend bool
	err := r.pg.QueryRow(ctx, `
		select exists (
		  select 1 from friends f
		  where f.user_id = $1 and f.friend_id = $2
		)
	`, viewerID, targetID).Scan(&isFriend)
	if err != nil {
		return dto.UserProfile{}, err
	}
	if !isFriend {
		return dto.UserProfile{}, errors.New("forbidden")
	}
	p, err := r.GetUserProfile(ctx, targetID)
	if err != nil {
		return dto.UserProfile{}, err
	}
	p.IsFriend = true
	return p, nil
}

func (r *Repository) SearchUsers(ctx context.Context, query string, excludeUserID int32, limit int) ([]dto.UserProfile, error) {
	if r.pg == nil {
		return nil, errors.New("repository: postgres not configured")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q := "%" + query + "%"
	rows, err := r.pg.Query(ctx, `
		select u.id, u.name, u.email,
		       coalesce(s.avatar_emoji, '') as emoji,
		       coalesce(s.avatar_bg, 0) as bg,
		       exists (
		         select 1 from friends f
		         where f.user_id = $1 and f.friend_id = u.id
		       ) as is_friend
		from users u
		left join user_settings s on s.user_id = u.id
		where u.id <> $1
		  and (u.name ilike $2 or u.email ilike $2)
		order by u.name asc
		limit $3
	`, excludeUserID, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dto.UserProfile
	for rows.Next() {
		var p dto.UserProfile
		if err := rows.Scan(&p.UserID, &p.Name, &p.Email, &p.Emoji, &p.BgIndex, &p.IsFriend); err != nil {
			return nil, err
		}
		if !p.IsFriend {
			p.Email = ""
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) ListFriends(ctx context.Context, userID int32) ([]dto.UserProfile, error) {
	if r.pg == nil {
		return nil, errors.New("repository: postgres not configured")
	}
	rows, err := r.pg.Query(ctx, `
		select u.id, u.name, u.email,
		       coalesce(s.avatar_emoji, '') as emoji,
		       coalesce(s.avatar_bg, 0) as bg
		from friends f
		join users u on u.id = f.friend_id
		left join user_settings s on s.user_id = u.id
		where f.user_id = $1
		order by u.name asc
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dto.UserProfile
	for rows.Next() {
		var p dto.UserProfile
		if err := rows.Scan(&p.UserID, &p.Name, &p.Email, &p.Emoji, &p.BgIndex); err != nil {
			return nil, err
		}
		p.IsFriend = true
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) CreateFriendRequest(ctx context.Context, fromUserID, toUserID int32) (dto.FriendRequest, error) {
	if r.pg == nil {
		return dto.FriendRequest{}, errors.New("repository: postgres not configured")
	}
	if fromUserID <= 0 || toUserID <= 0 || fromUserID == toUserID {
		return dto.FriendRequest{}, errors.New("repository: invalid user id")
	}

	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return dto.FriendRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// already friends?
	var exists int
	if err := tx.QueryRow(ctx, `
		select 1 from friends where user_id=$1 and friend_id=$2
	`, fromUserID, toUserID).Scan(&exists); err == nil {
		return dto.FriendRequest{}, errors.New("already friends")
	}

	var id int64
	err = tx.QueryRow(ctx, `
		insert into friend_requests (from_user_id, to_user_id, status)
		values ($1, $2, 'pending')
		on conflict (from_user_id, to_user_id) do update
		set status = 'pending', created_at = now()
		returning id
	`, fromUserID, toUserID).Scan(&id)
	if err != nil {
		return dto.FriendRequest{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return dto.FriendRequest{}, err
	}

	reqs, err := r.ListFriendRequests(ctx, toUserID, "pending")
	if err != nil {
		return dto.FriendRequest{}, err
	}
	for _, fr := range reqs {
		if fr.ID == id {
			return fr, nil
		}
	}
	return dto.FriendRequest{ID: id, Status: "pending"}, nil
}

func (r *Repository) ListFriendRequests(ctx context.Context, userID int32, status string) ([]dto.FriendRequest, error) {
	if r.pg == nil {
		return nil, errors.New("repository: postgres not configured")
	}
	if status == "" {
		status = "pending"
	}
	rows, err := r.pg.Query(ctx, `
		select fr.id, fr.status, fr.created_at,
		       u1.id, u1.name, u1.email, coalesce(s1.avatar_emoji, '🙂'), coalesce(s1.avatar_bg, 0),
		       u2.id, u2.name, u2.email, coalesce(s2.avatar_emoji, '🙂'), coalesce(s2.avatar_bg, 0)
		from friend_requests fr
		join users u1 on u1.id = fr.from_user_id
		left join user_settings s1 on s1.user_id = u1.id
		join users u2 on u2.id = fr.to_user_id
		left join user_settings s2 on s2.user_id = u2.id
		where fr.to_user_id = $1 and fr.status = $2
		order by fr.created_at desc
	`, userID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dto.FriendRequest
	for rows.Next() {
		var fr dto.FriendRequest
		var from dto.UserProfile
		var to dto.UserProfile
		if err := rows.Scan(
			&fr.ID, &fr.Status, &fr.CreatedAt,
			&from.UserID, &from.Name, &from.Email, &from.Emoji, &from.BgIndex,
			&to.UserID, &to.Name, &to.Email, &to.Emoji, &to.BgIndex,
		); err != nil {
			return nil, err
		}
		fr.From = from
		fr.To = to
		out = append(out, fr)
	}
	return out, rows.Err()
}

func (r *Repository) RespondFriendRequest(ctx context.Context, userID int32, requestID int64, action string) error {
	if r.pg == nil {
		return errors.New("repository: postgres not configured")
	}
	if userID <= 0 || requestID <= 0 {
		return errors.New("repository: invalid input")
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "accept" && action != "decline" {
		return errors.New("invalid action")
	}

	tx, err := r.pg.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var fromID, toID int32
	err = tx.QueryRow(ctx, `
		select from_user_id, to_user_id
		from friend_requests
		where id = $1 and status = 'pending'
	`, requestID).Scan(&fromID, &toID)
	if err != nil {
		return err
	}
	if toID != userID {
		return errors.New("forbidden")
	}

	if action == "accept" {
		_, err = tx.Exec(ctx, `
			insert into friends (user_id, friend_id)
			values ($1, $2), ($2, $1)
			on conflict do nothing
		`, fromID, toID)
		if err != nil {
			return err
		}
	}

	newStatus := "declined"
	if action == "accept" {
		newStatus = "accepted"
	}
	_, err = tx.Exec(ctx, `
		update friend_requests
		set status = $1
		where id = $2
	`, newStatus, requestID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
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
