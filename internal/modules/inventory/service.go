package inventory

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"barbershop/internal/audit"
	"barbershop/internal/httpx"
)

var pricePattern = regexp.MustCompile(`^\d{1,7}(\.\d{1,2})?$`)

type Manager struct {
	pool *pgxpool.Pool
	repo *Repository
}

func NewManager(pool *pgxpool.Pool) *Manager {
	return &Manager{pool: pool, repo: NewRepository(pool)}
}

type Actor struct {
	UserID string
	Role   string
	IP     string
}

func notFoundItem() *httpx.AppError {
	return httpx.NotFound("ไม่พบรายการสินค้านี้")
}

// ---- items ----

type ListItemsQuery struct {
	Query           string
	CategoryID      string
	LowStockOnly    bool
	IncludeInactive bool
	Page            int
	Limit           int
}

func (m *Manager) ListItems(ctx context.Context, q ListItemsQuery) ([]Item, int, error) {
	rows, total, err := m.repo.ListItems(ctx, ItemFilters{
		Query: q.Query, CategoryID: q.CategoryID, LowStockOnly: q.LowStockOnly,
		IncludeInactive: q.IncludeInactive, Offset: (q.Page - 1) * q.Limit, Limit: q.Limit,
	})
	if err != nil {
		return nil, 0, err
	}
	list := make([]Item, 0, len(rows))
	for _, r := range rows {
		list = append(list, toItemDTO(r))
	}
	return list, total, nil
}

func (m *Manager) GetItem(ctx context.Context, id string) (*Item, error) {
	row, err := m.repo.GetItemByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFoundItem()
	}
	if err != nil {
		return nil, err
	}
	item := toItemDTO(*row)
	return &item, nil
}

func (m *Manager) GetByBarcode(ctx context.Context, barcode string) (*Item, error) {
	row, err := m.repo.GetItemByBarcode(ctx, barcode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	item := toItemDTO(*row)
	return &item, nil
}

func (m *Manager) CreateItem(ctx context.Context, req CreateItemRequest, actor Actor) (*Item, error) {
	if err := validateCost(req.CostPrice); err != nil {
		return nil, err
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	var categoryID *string
	if req.CategoryID != nil && *req.CategoryID != "" {
		categoryID = req.CategoryID
	}
	barcode := emptyToNil(req.Barcode)
	if barcode != nil {
		owner, err := m.repo.BarcodeOwnerName(ctx, *barcode, "")
		if err != nil {
			return nil, err
		}
		if owner != nil {
			return nil, httpx.Conflict(fmt.Sprintf("บาร์โค้ดนี้ถูกใช้โดยสินค้า %q อยู่แล้ว", *owner))
		}
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO inventory_items (name, category_id, unit, quantity, min_quantity, cost_price, image_url, barcode, is_active, note)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6,'')::numeric, $7, $8, $9, NULLIF($10,''))
		RETURNING id`,
		req.Name, categoryID, req.Unit, req.Quantity, req.MinQuantity, deref(req.CostPrice), emptyToNil(req.ImageURL), barcode, isActive, emptyToNil(req.Note),
	).Scan(&id); err != nil {
		return nil, err
	}

	// ยอดเริ่มต้นต้องมี movement คู่กันเสมอ — ไม่มีการตั้ง quantity โดยไม่มีประวัติ
	if req.Quantity > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO stock_movements (item_id, type, quantity_change, balance_after, reason, performed_by)
			VALUES ($1, 'in', $2, $2, $3, NULLIF($4,'')::uuid)`,
			id, req.Quantity, "ยอดยกมาเริ่มต้น", actor.UserID); err != nil {
			return nil, err
		}
	}

	audit.Log(ctx, tx, strPtr(actor.UserID), "inventory.item_created", "inventory_item", id,
		map[string]any{"name": req.Name, "initial_quantity": req.Quantity}, actor.IP)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return m.GetItem(ctx, id)
}

func (m *Manager) UpdateItem(ctx context.Context, id string, req UpdateItemRequest, actor Actor) (*Item, error) {
	cur, err := m.repo.GetItemByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFoundItem()
	}
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		cur.Name = *req.Name
	}
	if req.CategoryID != nil {
		if *req.CategoryID == "" {
			cur.CategoryID = nil
		} else {
			cur.CategoryID = req.CategoryID
		}
	}
	if req.Unit != nil {
		cur.Unit = *req.Unit
	}
	if req.MinQuantity != nil {
		cur.MinQuantity = *req.MinQuantity
	}
	if req.CostPrice != nil {
		if err := validateCost(req.CostPrice); err != nil {
			return nil, err
		}
		if strings.TrimSpace(*req.CostPrice) == "" {
			cur.CostPrice = nil
		} else {
			cur.CostPrice = req.CostPrice
		}
	}
	if req.ImageURL != nil {
		cur.ImageURL = emptyToNil(req.ImageURL)
	}
	if req.Barcode != nil {
		newBarcode := emptyToNil(req.Barcode)
		if newBarcode != nil && (cur.Barcode == nil || *newBarcode != *cur.Barcode) {
			owner, err := m.repo.BarcodeOwnerName(ctx, *newBarcode, cur.ID)
			if err != nil {
				return nil, err
			}
			if owner != nil {
				return nil, httpx.Conflict(fmt.Sprintf("บาร์โค้ดนี้ถูกใช้โดยสินค้า %q อยู่แล้ว", *owner))
			}
		}
		cur.Barcode = newBarcode
	}
	if req.Note != nil {
		cur.Note = emptyToNil(req.Note)
	}
	if req.IsActive != nil {
		cur.IsActive = *req.IsActive
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE inventory_items SET
			name = $2, category_id = $3, unit = $4, min_quantity = $5,
			cost_price = NULLIF($6,'')::numeric, image_url = $7, barcode = $8, is_active = $9, note = $10, updated_at = now()
		WHERE id = $1`,
		id, cur.Name, cur.CategoryID, cur.Unit, cur.MinQuantity, deref(cur.CostPrice), cur.ImageURL, cur.Barcode, cur.IsActive, cur.Note,
	); err != nil {
		return nil, err
	}
	audit.Log(ctx, tx, strPtr(actor.UserID), "inventory.item_updated", "inventory_item", id, nil, actor.IP)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return m.GetItem(ctx, id)
}

// Deactivate ปิดใช้งานสินค้า (soft) — ประวัติ movement คงอยู่ครบ
func (m *Manager) DeactivateItem(ctx context.Context, id string, actor Actor) error {
	cur, err := m.repo.GetItemByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFoundItem()
	}
	if err != nil {
		return err
	}
	cur.IsActive = false
	_, err = m.pool.Exec(ctx, `
		UPDATE inventory_items SET is_active = false, updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "inventory.item_deactivated", "inventory_item", id,
		map[string]any{"name": cur.Name}, actor.IP)
	return nil
}

// ---- movements ----

// ApplyMovement เปลี่ยนสต็อกผ่านรายการ movement ใน transaction เดียวพร้อม row lock
// quantity ของ adjustment หมายถึง "ยอดใหม่ที่นับได้" ส่วนชนิดอื่นหมายถึง "จำนวนที่เปลี่ยน"
func (m *Manager) ApplyMovement(ctx context.Context, req MovementRequest, actor Actor) (*Item, *MovementRecord, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	// ล็อกแถวสินค้ากันการเบิกพร้อมกันจนยอดผิด
	var current int
	err = tx.QueryRow(ctx, `SELECT quantity FROM inventory_items WHERE id = $1 FOR UPDATE`, req.ItemID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, notFoundItem()
	}
	if err != nil {
		return nil, nil, err
	}

	newBalance := 0
	switch req.Type {
	case "in":
		newBalance = current + req.Quantity
	case "out", "damage":
		newBalance = current - req.Quantity
		if newBalance < 0 {
			return nil, nil, httpx.BadRequest(
				fmt.Sprintf("สต็อกไม่พอ — คงเหลืออยู่ %d หน่วย แต่ต้องการ %d หน่วย", current, req.Quantity))
		}
	case "adjustment":
		newBalance = req.Quantity // ปรับยอดจากการตรวจนับ = ยอดใหม่
	default:
		return nil, nil, httpx.BadRequest("ประเภทรายการไม่ถูกต้อง")
	}
	change := newBalance - current
	if change == 0 {
		return nil, nil, httpx.BadRequest("ยอดใหม่ไม่มีการเปลี่ยนแปลง")
	}

	if _, err := tx.Exec(ctx, `UPDATE inventory_items SET quantity = $2, updated_at = now() WHERE id = $1`,
		req.ItemID, newBalance); err != nil {
		return nil, nil, err
	}

	var movementID string
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO stock_movements (item_id, type, quantity_change, balance_after, reason, performed_by)
		VALUES ($1, $2::movement_type, $3, $4, $5, NULLIF($6,'')::uuid)
		RETURNING id, created_at`,
		req.ItemID, req.Type, change, newBalance, req.Reason, actor.UserID,
	).Scan(&movementID, &createdAt); err != nil {
		return nil, nil, err
	}

	audit.Log(ctx, tx, strPtr(actor.UserID), "inventory.movement", "inventory_item", req.ItemID, map[string]any{
		"type": req.Type, "change": change, "balance_after": newBalance, "reason": req.Reason,
	}, actor.IP)

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}

	item, err := m.GetItem(ctx, req.ItemID)
	if err != nil {
		return nil, nil, err
	}
	record := &MovementRecord{
		ID: movementID, ItemID: req.ItemID, Type: req.Type,
		QuantityChange: change, BalanceAfter: newBalance, Reason: req.Reason, CreatedAt: createdAt.Format(time.RFC3339),
	}
	return item, record, nil
}

func (m *Manager) ListMovements(ctx context.Context, itemID string, page, limit int) ([]MovementRecord, int, error) {
	return m.repo.ListMovements(ctx, itemID, (page-1)*limit, limit)
}

// ---- categories ----

func (m *Manager) ListCategories(ctx context.Context) ([]Category, error) {
	return m.repo.ListCategories(ctx)
}

func (m *Manager) CreateCategory(ctx context.Context, name string, actor Actor) (*Category, error) {
	name = strings.TrimSpace(name)
	exists, err := m.repo.CategoryNameExists(ctx, name, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, httpx.Conflict("มีหมวดหมู่ชื่อนี้อยู่แล้ว")
	}
	var c Category
	if err := m.pool.QueryRow(ctx,
		`INSERT INTO inventory_categories (name) VALUES ($1) RETURNING id, name`, name).Scan(&c.ID, &c.Name); err != nil {
		return nil, err
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "inventory.category_created", "inventory_category", c.ID, nil, actor.IP)
	return &c, nil
}

func (m *Manager) UpdateCategory(ctx context.Context, id, name string, actor Actor) (*Category, error) {
	name = strings.TrimSpace(name)
	exists, err := m.repo.CategoryNameExists(ctx, name, id)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, httpx.Conflict("มีหมวดหมู่ชื่อนี้อยู่แล้ว")
	}
	var c Category
	err = m.pool.QueryRow(ctx,
		`UPDATE inventory_categories SET name = $2 WHERE id = $1 RETURNING id, name`, id, name).Scan(&c.ID, &c.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ไม่พบหมวดหมู่นี้")
	}
	if err != nil {
		return nil, err
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "inventory.category_updated", "inventory_category", id, nil, actor.IP)
	return &c, nil
}

func (m *Manager) DeleteCategory(ctx context.Context, id string, actor Actor) error {
	tag, err := m.pool.Exec(ctx, `DELETE FROM inventory_categories WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("ไม่พบหมวดหมู่นี้")
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "inventory.category_deleted", "inventory_category", id, nil, actor.IP)
	return nil
}

// ---- helpers ----

func toItemDTO(r ItemRow) Item {
	return Item{
		ID: r.ID, Name: r.Name, CategoryID: r.CategoryID, CategoryName: r.CategoryName,
		Unit: r.Unit, Quantity: r.Quantity, MinQuantity: r.MinQuantity,
		CostPrice: r.CostPrice, ImageURL: r.ImageURL, Barcode: r.Barcode,
		IsActive: r.IsActive, Note: r.Note,
	}
}

func validateCost(cost *string) *httpx.AppError {
	if cost == nil || strings.TrimSpace(*cost) == "" {
		return nil
	}
	if !pricePattern.MatchString(*cost) {
		return httpx.BadRequest("รูปแบบต้นทุนไม่ถูกต้อง กรุณาระบุเป็นตัวเลข เช่น 180 หรือ 180.50")
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func emptyToNil(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}

func strPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
