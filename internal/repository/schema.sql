create table if not exists analyses (
	id text primary key,
	request jsonb not null,
	response jsonb not null,
	created_at timestamptz not null default now()
);

create index if not exists analyses_created_at_idx on analyses (created_at desc);
