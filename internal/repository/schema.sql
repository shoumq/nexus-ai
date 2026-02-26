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
	updated_at timestamptz not null default now()
);

create table if not exists last_analyses (
	user_id int not null,
	period text not null,
	response jsonb not null,
	updated_at timestamptz not null default now(),
	primary key (user_id, period)
);

create table if not exists track_points (
	id bigserial primary key,
	user_id int not null,
	ts timestamptz not null,
	sleep_hours double precision not null,
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
	time_bucket_5m bigint not null,
	created_at timestamptz not null default now()
);

create index if not exists track_points_user_ts_idx on track_points (user_id, ts desc);
create unique index if not exists track_points_user_bucket_uniq on track_points (user_id, time_bucket_5m);

alter table track_points
	add column if not exists time_bucket_5m bigint not null default 0;

alter table track_points
	add column if not exists stress double precision not null default 0;
alter table track_points
	add column if not exists energy double precision not null default 0;
alter table track_points
	add column if not exists concentration double precision not null default 0;
alter table track_points
	add column if not exists sleep_quality double precision not null default 0;
alter table track_points
	add column if not exists caffeine boolean not null default false;
alter table track_points
	add column if not exists alcohol boolean not null default false;
alter table track_points
	add column if not exists workout boolean not null default false;
alter table track_points
	add column if not exists llm_text text not null default '';

alter table user_settings
	add column if not exists user_tz text not null default 'UTC';

alter table last_analyses
	add column if not exists response jsonb not null default '{}'::jsonb;
