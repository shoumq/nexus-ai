-- +goose Up
ALTER TABLE track_points
    ADD COLUMN IF NOT EXISTS sleep_start text NOT NULL DEFAULT '';
ALTER TABLE track_points
    ADD COLUMN IF NOT EXISTS sleep_end text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE track_points
    DROP COLUMN IF EXISTS sleep_start;
ALTER TABLE track_points
    DROP COLUMN IF EXISTS sleep_end;
