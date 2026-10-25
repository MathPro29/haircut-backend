package services

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const serviceColumns = `id, name, slug, description, price::text, duration_minutes, image_url, is_active, is_featured, display_order`

const listSQL = `SELECT ` + serviceColumns + ` FROM services`

func (r *Repository) List(ctx context.Context, activeOnly bool) ([]Service, error) {
	sql := listSQL + ` ORDER BY display_order, name`
	if activeOnly {
		sql = `SELECT ` + serviceColumns + ` FROM services WHERE is_active = true ORDER BY display_order, name`
	}
	rows, err := r.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Service
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *s)
	}
	return list, rows.Err()
}

func (r *Repository) GetByID(ctx context.Context, id string) (*Service, error) {
	row := r.pool.QueryRow(ctx, listSQL+` WHERE id = $1`, id)
	s, err := scanService(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	return s, err
}

func (r *Repository) SlugExists(ctx context.Context, slug, excludeID string) (bool, error) {
	// excludeID ว่าง = กรณีสร้างใหม่ (ยังไม่มี id ของตัวเอง) ต้องเช็คทุกแถว
	sql := `SELECT EXISTS(SELECT 1 FROM services WHERE slug = $1)`
	args := []any{slug}
	if excludeID != "" {
		sql += ` AND id <> $2`
		args = append(args, excludeID)
	}
	var exists bool
	err := r.pool.QueryRow(ctx, sql, args...).Scan(&exists)
	return exists, err
}

func (r *Repository) Create(ctx context.Context, s *Service) error {
	return r.pool.QueryRow(ctx, `
		INSERT INTO services (name, slug, description, price, duration_minutes, image_url, is_active, is_featured, display_order)
		VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8, $9)
		RETURNING id`,
		s.Name, s.Slug, s.Description, s.Price, s.DurationMinutes, s.ImageURL, s.IsActive, s.IsFeatured, s.DisplayOrder,
	).Scan(&s.ID)
}

func (r *Repository) Update(ctx context.Context, s *Service) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE services SET
			name = $2, slug = $3, description = $4, price = $5::numeric, duration_minutes = $6,
			image_url = $7, is_active = $8, is_featured = $9, display_order = $10, updated_at = now()
		WHERE id = $1`,
		s.ID, s.Name, s.Slug, s.Description, s.Price, s.DurationMinutes, s.ImageURL, s.IsActive, s.IsFeatured, s.DisplayOrder,
	)
	return err
}

func scanService(row pgx.Row) (*Service, error) {
	var s Service
	err := row.Scan(&s.ID, &s.Name, &s.Slug, &s.Description, &s.Price, &s.DurationMinutes,
		&s.ImageURL, &s.IsActive, &s.IsFeatured, &s.DisplayOrder)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
