package dashboard

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"barbershop/internal/httpx"
)

type Summary struct {
	TodayBookings   int            `json:"todayBookings"`
	PendingBookings int            `json:"pendingBookings"`
	CompletedToday  int            `json:"completedToday"`
	CancelledToday  int            `json:"cancelledToday"`
	LowStockCount   int            `json:"lowStockCount"`
	LowStockItems   []LowStockItem `json:"lowStockItems"`
	RecentBookings  []RecentBooking `json:"recentBookings"`
}

type LowStockItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Quantity    int    `json:"quantity"`
	MinQuantity int    `json:"minQuantity"`
	Unit        string `json:"unit"`
}

type RecentBooking struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	CustomerName string `json:"customerName"`
	StaffName    string `json:"staffName"`
	StartTime    string `json:"startTime"`
	Status       string `json:"status"`
}

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

func (h *Handler) RegisterRoutes(api *gin.RouterGroup, authRequired gin.HandlerFunc) {
	api.GET("/dashboard/summary", authRequired, h.summary)
}

func (h *Handler) summary(c *gin.Context) {
	ctx := c.Request.Context()
	s, err := h.buildSummary(ctx)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, s)
}

func (h *Handler) buildSummary(ctx context.Context) (*Summary, error) {
	var tz string
	if err := h.pool.QueryRow(ctx, `SELECT timezone FROM store_settings WHERE id=1`).Scan(&tz); err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.FixedZone(tz, 7*3600)
	}
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	todayEnd := todayStart.AddDate(0, 0, 1)

	s := &Summary{LowStockItems: []LowStockItem{}, RecentBookings: []RecentBooking{}}

	if err := h.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE start_time >= $1 AND start_time < $2),
			count(*) FILTER (WHERE status = 'completed' AND start_time >= $1 AND start_time < $2),
			count(*) FILTER (WHERE status = 'cancelled' AND start_time >= $1 AND start_time < $2),
			count(*) FILTER (WHERE status = 'pending' AND start_time >= $1)
		FROM bookings`, todayStart, todayEnd,
	).Scan(&s.TodayBookings, &s.CompletedToday, &s.CancelledToday, &s.PendingBookings); err != nil {
		return nil, err
	}

	if err := h.pool.QueryRow(ctx, `
		SELECT count(*) FROM inventory_items
		WHERE is_active = true AND min_quantity > 0 AND quantity <= min_quantity`,
	).Scan(&s.LowStockCount); err != nil {
		return nil, err
	}

	rows, err := h.pool.Query(ctx, `
		SELECT id, name, quantity, min_quantity, unit FROM inventory_items
		WHERE is_active = true AND min_quantity > 0 AND quantity <= min_quantity
		ORDER BY quantity - min_quantity ASC, name
		LIMIT 6`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it LowStockItem
		if err := rows.Scan(&it.ID, &it.Name, &it.Quantity, &it.MinQuantity, &it.Unit); err != nil {
			return nil, err
		}
		s.LowStockItems = append(s.LowStockItems, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows2, err := h.pool.Query(ctx, `
		SELECT b.id, b.code, b.customer_name, s.full_name, b.start_time, b.status::text
		FROM bookings b JOIN staff s ON s.id = b.staff_id
		ORDER BY b.created_at DESC
		LIMIT 6`)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var b RecentBooking
		var start time.Time
		if err := rows2.Scan(&b.ID, &b.Code, &b.CustomerName, &b.StaffName, &start, &b.Status); err != nil {
			return nil, err
		}
		b.StartTime = start.Format(time.RFC3339)
		s.RecentBookings = append(s.RecentBookings, b)
	}
	return s, rows2.Err()
}
