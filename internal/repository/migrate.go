package repository

import (
	"context"
	_ "embed"
)

//go:embed schema.sql
var schemaSQL string

func (r *Repository) AutoMigrate(ctx context.Context) error {
	if r.pg == nil {
		return nil
	}
	_, err := r.pg.Exec(ctx, schemaSQL)
	return err
}
