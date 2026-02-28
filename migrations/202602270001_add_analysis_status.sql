-- +goose Up
alter table track_points
	add column if not exists analysis_status text not null default 'ready',
	add column if not exists analysis_updated_at timestamptz not null default now(),
	add column if not exists analysis_error text not null default '';

-- +goose Down
alter table track_points
	drop column if exists analysis_error,
	drop column if exists analysis_updated_at,
	drop column if exists analysis_status;
