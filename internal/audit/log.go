package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Executor รองรับทั้ง pgxpool และ pgx.Tx
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Log บันทึกประวัติการเปลี่ยนแปลงข้อมูลสำคัญ — fail แบบเงียบ (ไม่ให้กระทบ transaction หลัก) แต่มี log
func Log(ctx context.Context, ex Executor, userID *string, action, entityType, entityID string, changes any, ip string) {
	var changesJSON []byte
	if changes != nil {
		var err error
		changesJSON, err = json.Marshal(changes)
		if err != nil {
			slog.Warn("audit marshal failed", "error", err)
			changesJSON = nil
		}
	}
	_, err := ex.Exec(ctx, `
		INSERT INTO audit_logs (user_id, action, entity_type, entity_id, changes, ip, created_at)
		VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, $7)`,
		userID, action, entityType, entityID, changesJSON, ip, time.Now(),
	)
	if err != nil {
		slog.Warn("audit log insert failed", "action", action, "error", err)
	}
}
