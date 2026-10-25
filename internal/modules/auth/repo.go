package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type userRecord struct {
	ID           string
	Username     string
	PasswordHash string
	FullName     string
	Role         string
	StaffID      *string
	IsActive     bool
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const userColumns = `id, username, password_hash, full_name, role::text, staff_id, is_active`

func (r *Repository) GetByUsername(ctx context.Context, username string) (*userRecord, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username)
	return scanUser(row)
}

func (r *Repository) GetByID(ctx context.Context, id string) (*userRecord, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	return scanUser(row)
}

func (r *Repository) TouchLogin(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET last_login_at = $2, updated_at = $2 WHERE id = $1`, id, time.Now())
	return err
}

func scanUser(row pgx.Row) (*userRecord, error) {
	var u userRecord
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.StaffID, &u.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
