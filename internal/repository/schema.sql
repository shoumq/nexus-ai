create table if not exists analyses (
	id text primary key,
	request jsonb not null,
	response jsonb not null,
	created_at timestamptz not null default now()
);

create index if not exists analyses_created_at_idx on analyses (created_at desc);

create table if not exists track_points (
	id bigserial primary key,
	user_id int not null,
	ts timestamptz not null,
	sleep_hours double precision not null,
	mood double precision not null,
	activity double precision not null,
	productive double precision not null,
	time_bucket_5m bigint generated always as (floor(extract(epoch from ts) / 300)) stored,
	created_at timestamptz not null default now()
);

create index if not exists track_points_user_ts_idx on track_points (user_id, ts desc);
create unique index if not exists track_points_user_bucket_uniq on track_points (user_id, time_bucket_5m);

alter table track_points
	add column if not exists time_bucket_5m bigint generated always as (floor(extract(epoch from ts) / 300)) stored;
