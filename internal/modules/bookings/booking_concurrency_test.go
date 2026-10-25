package bookings

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration test — ต้องมี PostgreSQL (docker compose up -d) ที่ DATABASE_URL
// ทดสอบข้อกำหนดหลัก: ยิงจอง slot เดียวกันพร้อมกัน N request → สำเร็จได้ 1 เท่านั้น

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := "postgres://barbershop:barbershop@localhost:5432/barbershop?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("skip: cannot connect test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("skip: database not reachable: %v", err)
	}
	return pool
}

func TestConcurrentBookingSameSlot_AllowsOnlyOne(t *testing.T) {
	pool := testPool(t)
	defer pool.Close()
	ctx := context.Background()

	tomorrow := time.Now().AddDate(0, 0, 1)

	// fixtures: ช่าง 1 คน + กะ + บริการ 1 รายการ (ลบท้ายเทส)
	var staffID string
	err := pool.QueryRow(ctx, `
		INSERT INTO staff (full_name, nickname, is_active, is_bookable, display_order)
		VALUES ('zz-test ช่างทดสอบ', 'test', true, true, 999) RETURNING id`,
	).Scan(&staffID)
	if err != nil {
		t.Fatalf("insert test staff: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM staff WHERE id = $1`, staffID)

	if _, err := pool.Exec(ctx, `
		INSERT INTO staff_schedules (staff_id, weekday, start_time, end_time)
		VALUES ($1, $2, '09:00', '18:00')`, staffID, int(tomorrow.Weekday())); err != nil {
		t.Fatalf("insert test schedule: %v", err)
	}

	var serviceID string
	err = pool.QueryRow(ctx, `
		INSERT INTO services (name, slug, price, duration_minutes, is_active, display_order)
		VALUES ('zz-test บริการทดสอบ', 'zz-test-service', 100, 60, true, 999) RETURNING id`,
	).Scan(&serviceID)
	if err != nil {
		t.Fatalf("insert test service: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM services WHERE id = $1`, serviceID)

	mgr := NewManager(pool)
	dateStr := tomorrow.Format("2006-01-02")

	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successes, conflicts, otherErrors int
	var createdID string

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := CreateBookingRequest{
				ServiceIDs:    []string{serviceID},
				StaffID:       &staffID,
				Date:          dateStr,
				StartTime:     "11:00",
				CustomerName:  fmt.Sprintf("ลูกค้าทดสอบ %d", i),
				CustomerPhone: "0810000000",
			}
			detail, err := mgr.Create(ctx, req, &Actor{IP: "127.0.0.1"})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
				createdID = detail.ID
			} else {
				if isConflict(err) {
					conflicts++
				} else {
					otherErrors++
					t.Logf("unexpected error: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	// เก็บกวาดคิวที่สร้างสำเร็จ
	if createdID != "" {
		defer pool.Exec(ctx, `DELETE FROM bookings WHERE id = $1`, createdID)
	}

	if successes != 1 {
		t.Errorf("successes = %d, want exactly 1", successes)
	}
	if conflicts != n-1 {
		t.Errorf("conflicts = %d, want %d", conflicts, n-1)
	}
	if otherErrors > 0 {
		t.Errorf("unexpected errors = %d, want 0", otherErrors)
	}
}

func isConflict(err error) bool {
	type coder interface{ HTTPStatus() int }
	msg := err.Error()
	return contains(msg, "ถูกจองไปแล้ว") || contains(msg, "ไม่พร้อมให้จอง")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
