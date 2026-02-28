-- +goose Up
create table if not exists analyses (
	id text primary key,
	request jsonb not null,
	response jsonb not null,
	created_at timestamptz not null default now()
);

create index if not exists analyses_created_at_idx on analyses (created_at desc);

create table if not exists user_settings (
	user_id int primary key,
	user_tz text not null default 'UTC',
	avatar_emoji text not null default '🙂',
	avatar_bg int not null default 0,
	updated_at timestamptz not null default now()
);

create table if not exists last_analyses (
	user_id int not null,
	period text not null,
	response jsonb not null,
	updated_at timestamptz not null default now(),
	primary key (user_id, period)
);

create table if not exists friend_requests (
	id bigserial primary key,
	from_user_id int not null,
	to_user_id int not null,
	status text not null default 'pending',
	created_at timestamptz not null default now(),
	unique (from_user_id, to_user_id)
);

create index if not exists friend_requests_to_idx on friend_requests (to_user_id, status);
create index if not exists friend_requests_from_idx on friend_requests (from_user_id, status);

create table if not exists friends (
	user_id int not null,
	friend_id int not null,
	created_at timestamptz not null default now(),
	primary key (user_id, friend_id)
);

create index if not exists friends_user_idx on friends (user_id);

create table if not exists track_points (
	id bigserial primary key,
	user_id int not null,
	ts timestamptz not null,
	sleep_hours double precision not null,
	sleep_start text not null default '',
	sleep_end text not null default '',
	mood double precision not null,
	activity double precision not null,
	productive double precision not null,
	stress double precision not null default 0,
	energy double precision not null default 0,
	concentration double precision not null default 0,
	sleep_quality double precision not null default 0,
	caffeine boolean not null default false,
	alcohol boolean not null default false,
	workout boolean not null default false,
	llm_text text not null default '',
	analysis_status text not null default 'ready',
	analysis_updated_at timestamptz not null default now(),
	analysis_error text not null default '',
	time_bucket_5m bigint not null,
	created_at timestamptz not null default now()
);

create index if not exists track_points_user_ts_idx on track_points (user_id, ts desc);
create unique index if not exists track_points_user_bucket_uniq on track_points (user_id, time_bucket_5m);

-- +goose Down
drop index if exists track_points_user_bucket_uniq;
drop index if exists track_points_user_ts_idx;
drop table if exists track_points;
drop table if exists friends;
drop table if exists friend_requests;
drop table if exists last_analyses;
drop table if exists user_settings;
drop index if exists analyses_created_at_idx;
drop table if exists analyses;
