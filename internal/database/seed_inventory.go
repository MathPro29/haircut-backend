package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedInventory ตัวอย่างสต็อกสินค้า — มี 2 รายการใกล้หมด (qty <= min) เพื่อทดสอบ low-stock alert
func seedInventory(ctx context.Context, pool *pgxpool.Pool) error {
	categories := []string{"เครื่องมือช่าง", "ผลิตภัณฑ์ดูแลผม", "ของใช้ในร้าน"}
	catIDs := map[string]string{}
	for _, name := range categories {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO inventory_categories (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
			return err
		}
		catIDs[name] = id
	}

	type item struct {
		name, category, unit string
		qty, min             int
		cost                 string
	}
	items := []item{
		{"ครีมโกนหนวด", "ผลิตภัณฑ์ดูแลผม", "bottle", 6, 10, "180"},   // LOW
		{"แชมพูสระผม", "ผลิตภัณฑ์ดูแลผม", "bottle", 15, 5, "220"},
		{"วอกซ์จัดแต่งทรงผม", "ผลิตภัณฑ์ดูแลผม", "jar", 9, 4, "150"},
		{"ปอมเมดจัดหนวด", "ผลิตภัณฑ์ดูแลผม", "jar", 12, 4, "140"},
		{"มาล้อตัดผมไฟฟ้า", "เครื่องมือช่าง", "piece", 4, 2, "3500"},
		{"กรรไกรตัดผม", "เครื่องมือช่าง", "piece", 6, 3, "1200"},
		{"มีดโกนคม", "เครื่องมือช่าง", "piece", 5, 2, "850"},
		{"ผ้าคลุมตัดผม", "ของใช้ในร้าน", "piece", 20, 8, "90"},
		{"แอลกอฮอล์ล้างเครื่องมือ", "ของใช้ในร้าน", "bottle", 3, 4, "120"}, // LOW
		{"สเปรย์ทำความสะอาดเก้าอี้", "ของใช้ในร้าน", "bottle", 8, 2, "95"},
	}

	for _, it := range items {
		var itemID string
		err := pool.QueryRow(ctx, `
			INSERT INTO inventory_items (name, category_id, unit, quantity, min_quantity, cost_price, is_active)
			VALUES ($1, $2, $3, $4, $5, $6::numeric, true) RETURNING id`,
			it.name, catIDs[it.category], it.unit, it.qty, it.min, it.cost,
		).Scan(&itemID)
		if err != nil {
			return err
		}
		// ประวัติยอดตั้งต้น — ทุกยอดต้องมี movement ครบตามข้อกำหนด
		if _, err := pool.Exec(ctx, `
			INSERT INTO stock_movements (item_id, type, quantity_change, balance_after, reason)
			VALUES ($1, 'in', $2, $2, 'ยอดยกมาเริ่มใช้ระบบ')`, itemID, it.qty); err != nil {
			return err
		}
	}
	return nil
}
