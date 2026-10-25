package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ---- items ----

type ItemRow struct {
	ID           string
	Name         string
	CategoryID   *string
	CategoryName *string
	Unit         string
	Quantity     int
	MinQuantity  int
	CostPrice    *string
	ImageURL     *string
	Barcode      *string
	IsActive     bool
	Note         *string
}

type ItemFilters struct {
	Query           string
	CategoryID      string
	LowStockOnly    bool
	IncludeInactive bool
	Offset          int
	Limit           int
}

const itemColumns = `i.id, i.name, i.category_id, c.name, i.unit, i.quantity, i.min_quantity, i.cost_price::text, i.image_url, i.barcode, i.is_active, i.note`

func (r *Repository) ListItems(ctx context.Context, f ItemFilters) ([]ItemRow, int, error) {
	where := []string{"1=1"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if !f.IncludeInactive {
		where = append(where, "i.is_active = true")
	}
	if f.Query != "" {
		p := arg("%" + f.Query + "%")
		where = append(where, fmt.Sprintf("(i.name ILIKE %s OR i.barcode ILIKE %s)", p, p))
	}
	if f.CategoryID != "" {
		where = append(where, "i.category_id = "+arg(f.CategoryID))
	}
	if f.LowStockOnly {
		where = append(where, "i.quantity <= i.min_quantity")
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM inventory_items i LEFT JOIN inventory_categories c ON c.id = i.category_id WHERE `+whereSQL,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT `+itemColumns+` FROM inventory_items i
		LEFT JOIN inventory_categories c ON c.id = i.category_id
		WHERE `+whereSQL+`
		ORDER BY i.name
		LIMIT `+arg(f.Limit)+` OFFSET `+arg(f.Offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := []ItemRow{}
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, *item)
	}
	return list, total, rows.Err()
}

func (r *Repository) GetItemByID(ctx context.Context, id string) (*ItemRow, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+itemColumns+` FROM inventory_items i
		LEFT JOIN inventory_categories c ON c.id = i.category_id
		WHERE i.id = $1`, id)
	item, err := scanItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	return item, err
}

func (r *Repository) GetItemByBarcode(ctx context.Context, barcode string) (*ItemRow, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+itemColumns+` FROM inventory_items i
		LEFT JOIN inventory_categories c ON c.id = i.category_id
		WHERE i.barcode = $1`, barcode)
	item, err := scanItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	return item, err
}

// BarcodeOwnerName ชื่อสินค้าอื่นที่ใช้บาร์โค้ดนี้อยู่ (เว้น id ที่ระบุ — ว่าง = เช็คทุกแถว)
func (r *Repository) BarcodeOwnerName(ctx context.Context, barcode, excludeID string) (*string, error) {
	q := `SELECT name FROM inventory_items WHERE barcode = $1`
	args := []any{barcode}
	if excludeID != "" {
		q += ` AND id <> $2`
		args = append(args, excludeID)
	}
	var name string
	err := r.pool.QueryRow(ctx, q, args...).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &name, nil
}

func scanItem(row pgx.Row) (*ItemRow, error) {
	var it ItemRow
	err := row.Scan(&it.ID, &it.Name, &it.CategoryID, &it.CategoryName, &it.Unit, &it.Quantity,
		&it.MinQuantity, &it.CostPrice, &it.ImageURL, &it.Barcode, &it.IsActive, &it.Note)
	if err != nil {
		return nil, err
	}
	return &it, nil
}

// ---- categories ----

func (r *Repository) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name FROM inventory_categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (r *Repository) CategoryNameExists(ctx context.Context, name, excludeID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM inventory_categories WHERE name = $1 AND id <> NULLIF($2,'')::uuid)`,
		name, excludeID).Scan(&exists)
	return exists, err
}

// ---- movements ----

func (r *Repository) ListMovements(ctx context.Context, itemID string, offset, limit int) ([]MovementRecord, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM stock_movements WHERE item_id = $1`, itemID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.item_id, m.type::text, m.quantity_change, m.balance_after, m.reason, u.full_name, m.created_at
		FROM stock_movements m
		LEFT JOIN users u ON u.id = m.performed_by
		WHERE m.item_id = $1
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $2 OFFSET $3`, itemID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := []MovementRecord{}
	for rows.Next() {
		var m MovementRecord
		var createdAt time.Time
		if err := rows.Scan(&m.ID, &m.ItemID, &m.Type, &m.QuantityChange, &m.BalanceAfter, &m.Reason, &m.PerformedByName, &createdAt); err != nil {
			return nil, 0, err
		}
		m.CreatedAt = createdAt.Format(time.RFC3339)
		list = append(list, m)
	}
	return list, total, rows.Err()
}
