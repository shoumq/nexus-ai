-- +goose Up
alter table user_settings
	add column if not exists avatar_emoji text not null default '🙂',
	add column if not exists avatar_bg int not null default 0;

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

-- +goose Down
drop table if exists friends;
drop table if exists friend_requests;
alter table user_settings
	drop column if exists avatar_bg,
	drop column if exists avatar_emoji;
