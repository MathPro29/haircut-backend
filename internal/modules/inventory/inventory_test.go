package inventory

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgres://barbershop:barbershop@localhost:5432/barbershop?sslmode=disable")
	if err != nil {
		t.Skipf("skip: cannot connect test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("skip: database not reachable: %v", err)
	}
	return pool
}

// ข้อกำหนดหลักของ Phase 4: เบิกสินค้าพร้อมกันหลายรายการ
// - ยอดสุดท้ายถูกต้อง ไม่ติดลบ
// - จำนวนรายการที่สำเร็จตรงกับยอดที่เบิกได้จริง
// - ทุก movement มี balance_after ไล่ลำดับถูกต้อง
func TestConcurrentStockOut_NeverNegative(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()

	// fixture: สินค้า 5 หน่วย เบิกพร้อมกัน 10 รายการ → สำเร็จได้ 5, ล้ม 5, เหลือ 0
	var itemID string
	err := pool.QueryRow(ctx, `
		INSERT INTO inventory_items (name, unit, quantity, min_quantity, is_active)
		VALUES ('zz-test สินค้าทดสอบ', 'piece', 5, 0, true) RETURNING id`,
	).Scan(&itemID)
	if err != nil {
		t.Fatalf("insert test item: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM stock_movements WHERE item_id = $1`, itemID)
	defer pool.Exec(ctx, `DELETE FROM inventory_items WHERE id = $1`, itemID)

	mgr := NewManager(pool)

	const n = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successes, insufficient int

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := mgr.ApplyMovement(ctx, MovementRequest{
				ItemID:   itemID,
				Type:     "out",
				Quantity: 1,
				Reason:   fmt.Sprintf("เบิกทดสอบ %d", i),
			}, Actor{UserID: "", Role: "owner", IP: "127.0.0.1"})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
			} else if err.Error() != "" && containsStr(err.Error(), "สต็อกไม่พอ") {
				insufficient++
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if successes != 5 {
		t.Errorf("successes = %d, want 5", successes)
	}
	if insufficient != 5 {
		t.Errorf("insufficient rejections = %d, want 5", insufficient)
	}

	// ยอดสุดท้ายต้องเป็น 0
	mgr2 := NewManager(pool)
	item, err := mgr2.GetItem(ctx, itemID)
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if item.Quantity != 0 {
		t.Errorf("final quantity = %d, want 0", item.Quantity)
	}

	// ประวัติ movement ต้องครบ 5 รายการ (ไม่นับยอดตั้งต้นเพราะ insert ตรง) และ balance ไล่ 4,3,2,1,0
	movements, _, err := mgr2.repo.ListMovements(ctx, itemID, 0, 100)
	if err != nil {
		t.Fatalf("list movements: %v", err)
	}
	if len(movements) != 5 {
		t.Fatalf("movements = %d, want 5", len(movements))
	}
	balances := map[int]bool{}
	for _, m := range movements {
		if m.QuantityChange != -1 {
			t.Errorf("movement change = %d, want -1", m.QuantityChange)
		}
		balances[m.BalanceAfter] = true
	}
	for b := 0; b <= 4; b++ {
		if !balances[b] {
			t.Errorf("missing balance_after %d in movement history", b)
		}
	}
}

func TestAdjustmentSetsNewBalance(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()

	var itemID string
	err := pool.QueryRow(ctx, `
		INSERT INTO inventory_items (name, unit, quantity, min_quantity, is_active)
		VALUES ('zz-test ปรับยอด', 'piece', 10, 0, true) RETURNING id`,
	).Scan(&itemID)
	if err != nil {
		t.Fatalf("insert test item: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM stock_movements WHERE item_id = $1`, itemID)
	defer pool.Exec(ctx, `DELETE FROM inventory_items WHERE id = $1`, itemID)

	mgr := NewManager(pool)
	item, record, err := mgr.ApplyMovement(ctx, MovementRequest{
		ItemID: itemID, Type: "adjustment", Quantity: 7, Reason: "ตรวจนับจริงได้ 7",
	}, Actor{Role: "owner"})
	if err != nil {
		t.Fatalf("apply adjustment: %v", err)
	}
	if item.Quantity != 7 {
		t.Errorf("quantity after adjustment = %d, want 7", item.Quantity)
	}
	if record.QuantityChange != -3 || record.BalanceAfter != 7 {
		t.Errorf("record = %+v, want change -3 balance 7", record)
	}

	// ยอดใหม่เท่าเดิม → ปฏิเสธ
	if _, _, err := mgr.ApplyMovement(ctx, MovementRequest{
		ItemID: itemID, Type: "adjustment", Quantity: 7, Reason: "ปรับเท่าเดิม",
	}, Actor{Role: "owner"}); err == nil {
		t.Error("adjustment to same balance should be rejected")
	}
}

func TestDamageReducesStock(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()

	var itemID string
	err := pool.QueryRow(ctx, `
		INSERT INTO inventory_items (name, unit, quantity, min_quantity, is_active)
		VALUES ('zz-test ชำรุด', 'piece', 4, 0, true) RETURNING id`,
	).Scan(&itemID)
	if err != nil {
		t.Fatalf("insert test item: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM stock_movements WHERE item_id = $1`, itemID)
	defer pool.Exec(ctx, `DELETE FROM inventory_items WHERE id = $1`, itemID)

	mgr := NewManager(pool)
	item, _, err := mgr.ApplyMovement(ctx, MovementRequest{
		ItemID: itemID, Type: "damage", Quantity: 2, Reason: "ขวดแตกระหว่างขนส่ง",
	}, Actor{Role: "owner"})
	if err != nil {
		t.Fatalf("apply damage: %v", err)
	}
	if item.Quantity != 2 {
		t.Errorf("quantity after damage = %d, want 2", item.Quantity)
	}
}

func TestCreateItemWithInitialQuantity(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()

	mgr := NewManager(pool)
	item, err := mgr.CreateItem(ctx, CreateItemRequest{
		Name: "zz-test สร้างพร้อมยอด", Unit: "piece", Quantity: 25, MinQuantity: 5, CostPrice: strPtr("99"),
	}, Actor{Role: "owner"})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM stock_movements WHERE item_id = $1`, item.ID)
	defer pool.Exec(ctx, `DELETE FROM inventory_items WHERE id = $1`, item.ID)

	// ยอดสินค้าต้องเท่ากับ movement ตั้งต้นเสมอ — เคยมีบั๊ก quantity ค้างเป็น 0
	if item.Quantity != 25 {
		t.Errorf("item.Quantity = %d, want 25", item.Quantity)
	}
	movements, _, err := mgr.repo.ListMovements(ctx, item.ID, 0, 10)
	if err != nil {
		t.Fatalf("list movements: %v", err)
	}
	if len(movements) != 1 || movements[0].QuantityChange != 25 || movements[0].BalanceAfter != 25 {
		t.Errorf("initial movement = %+v, want single in/+25/balance 25", movements)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
