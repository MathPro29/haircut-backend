package bookings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"barbershop/internal/modules/settings"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ---- store config ----

type StoreConfig struct {
	Timezone                string
	MaxAdvanceDays          int
	AutoConfirm             bool
	CancellationCutoffMin   int
	OpeningHours            map[string]settings.DayHours
}

func (r *Repository) GetStoreConfig(ctx context.Context) (*StoreConfig, error) {
	cfg := &StoreConfig{
		OpeningHours: map[string]settings.DayHours{},
	}
	err := r.pool.QueryRow(ctx, `
		SELECT timezone, max_advance_days, auto_confirm_bookings, cancellation_cutoff_minutes, opening_hours
		FROM store_settings WHERE id = 1`,
	).Scan(&cfg.Timezone, &cfg.MaxAdvanceDays, &cfg.AutoConfirm, &cfg.CancellationCutoffMin, &cfg.OpeningHours)
	if err != nil {
		return nil, fmt.Errorf("load store config: %w", err)
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "Asia/Bangkok"
	}
	return cfg, nil
}

func (r *Repository) IsClosedDate(ctx context.Context, date time.Time) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM store_closures WHERE closure_date = $1)`, date,
	).Scan(&exists)
	return exists, err
}

// ---- staff ----

type StaffRow struct {
	ID           string
	FullName     string
	Nickname     *string
	AvatarURL    *string
	DisplayOrder int
}

// ListBookableStaff ช่างที่ active + เปิดรับจอง (ระบุ IDs = เฉพาะคนนั้น)
func (r *Repository) ListBookableStaff(ctx context.Context, ids []string) ([]StaffRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, full_name, nickname, avatar_url, display_order FROM staff
		WHERE is_active = true AND is_bookable = true
		ORDER BY display_order, full_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []StaffRow
	for rows.Next() {
		var s StaffRow
		if err := rows.Scan(&s.ID, &s.FullName, &s.Nickname, &s.AvatarURL, &s.DisplayOrder); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	if ids == nil {
		return list, rows.Err()
	}
	// filter ตาม ids ที่ระบุ
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	filtered := list[:0]
	for _, s := range list {
		if want[s.ID] {
			filtered = append(filtered, s)
		}
	}
	return filtered, rows.Err()
}

// GetScheduleWindow ตารางกะของ staff ใน weekday นั้น — คืนเป็น (start, end, found)
func (r *Repository) GetScheduleWindow(ctx context.Context, staffIDs []string, weekday int) (map[string]Interval, error) {
	result := map[string]Interval{}
	if len(staffIDs) == 0 {
		return result, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT staff_id,
		       EXTRACT(EPOCH FROM start_time)::int / 60 AS start_min,
		       EXTRACT(EPOCH FROM end_time)::int / 60 AS end_min
		FROM staff_schedules WHERE weekday = $1 AND staff_id = ANY($2)`, weekday, staffIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var staffID string
		var startMin, endMin int
		if err := rows.Scan(&staffID, &startMin, &endMin); err != nil {
			return nil, err
		}
		result[staffID] = Interval{Start: startMin, End: endMin}
	}
	return result, rows.Err()
}

// GetTimeOffStaffIDs ช่างที่ลา/หยุดครอบคลุมวันนั้น
func (r *Repository) GetTimeOffStaffIDs(ctx context.Context, staffIDs []string, date time.Time) (map[string]bool, error) {
	result := map[string]bool{}
	if len(staffIDs) == 0 {
		return result, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT staff_id FROM staff_time_off
		WHERE staff_id = ANY($1) AND start_date <= $2 AND end_date >= $2`, staffIDs, date,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

// GetBusyIntervals คิว active ของ staff ในวันนั้น เป็นช่วงนาทีจากเที่ยงคืน (เวลาท้องถิ่นของร้าน)
func (r *Repository) GetBusyIntervals(ctx context.Context, staffIDs []string, dayStart time.Time, excludeBookingID string) (map[string][]Interval, map[string]int, error) {
	result := map[string][]Interval{}
	counts := map[string]int{}
	if len(staffIDs) == 0 {
		return result, counts, nil
	}
	args := []any{staffIDs, dayStart, dayStart.Add(24 * time.Hour)}
	q := `
		SELECT staff_id, start_time, end_time FROM bookings
		WHERE staff_id = ANY($1) AND status IN ('pending','confirmed')
		  AND start_time < $3 AND end_time > $2`
	if excludeBookingID != "" {
		q += ` AND id <> $4`
		args = append(args, excludeBookingID)
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var staffID string
		var start, end time.Time
		if err := rows.Scan(&staffID, &start, &end); err != nil {
			return nil, nil, err
		}
		sMin := int(start.Sub(dayStart).Minutes())
		eMin := int(end.Sub(dayStart).Minutes())
		if sMin < 0 {
			sMin = 0
		}
		if eMin > 24*60 {
			eMin = 24 * 60
		}
		result[staffID] = append(result[staffID], Interval{Start: sMin, End: eMin})
		counts[staffID]++
	}
	return result, counts, rows.Err()
}

// ---- services ----

type ServiceRow struct {
	ID              string
	Name            string
	Price           string
	DurationMinutes int
	IsActive        bool
}

func (r *Repository) GetServicesByIDs(ctx context.Context, ids []string) ([]ServiceRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, price::text, duration_minutes, is_active FROM services WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ServiceRow
	for rows.Next() {
		var s ServiceRow
		if err := rows.Scan(&s.ID, &s.Name, &s.Price, &s.DurationMinutes, &s.IsActive); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// ---- bookings ----

type BookingRow struct {
	ID              string
	Code            string
	CustomerName    string
	CustomerPhone   string
	Note            *string
	StaffID         string
	StartTime       time.Time
	EndTime         time.Time
	Status          string
	Source          string
	CancelledReason *string
	CreatedAt       time.Time
}

const bookingColumns = `id, code, customer_name, customer_phone, note, staff_id, start_time, end_time, status::text, source::text, cancelled_reason, created_at`

func (r *Repository) GetBookingByID(ctx context.Context, id string) (*BookingRow, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings WHERE id = $1`, id)
	return scanBooking(row)
}

func (r *Repository) GetBookingByCode(ctx context.Context, code string) (*BookingRow, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings WHERE code = $1`, code)
	return scanBooking(row)
}

func scanBooking(row pgx.Row) (*BookingRow, error) {
	var b BookingRow
	err := row.Scan(&b.ID, &b.Code, &b.CustomerName, &b.CustomerPhone, &b.Note, &b.StaffID,
		&b.StartTime, &b.EndTime, &b.Status, &b.Source, &b.CancelledReason, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *Repository) GetBookingServices(ctx context.Context, bookingID string) ([]BookingServiceItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT service_id, service_name, price::text, duration_minutes FROM booking_services
		WHERE booking_id = $1 ORDER BY service_name`, bookingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []BookingServiceItem{}
	for rows.Next() {
		var item BookingServiceItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Price, &item.DurationMinutes); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

type BookingListFilters struct {
	Search  string
	StaffID string
	Status  string
	From    *time.Time
	To      *time.Time
	Offset  int
	Limit   int
}

func (r *Repository) ListBookings(ctx context.Context, f BookingListFilters) ([]BookingSummary, int, error) {
	where := []string{"1=1"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Search != "" {
		p := arg("%" + f.Search + "%")
		where = append(where, fmt.Sprintf("(customer_name ILIKE %s OR customer_phone ILIKE %s OR code ILIKE %s)", p, p, p))
	}
	if f.StaffID != "" {
		where = append(where, "staff_id = "+arg(f.StaffID))
	}
	if f.Status != "" {
		where = append(where, "status = "+arg(f.Status)+"::booking_status")
	}
	if f.From != nil {
		where = append(where, "start_time >= "+arg(*f.From))
	}
	if f.To != nil {
		where = append(where, "start_time < "+arg(*f.To))
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM bookings WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := `
		SELECT b.id, b.code, b.customer_name, b.customer_phone, s.full_name,
		       b.start_time, b.end_time, b.status::text, b.source::text, b.note,
		       COALESCE(json_agg(bs.service_name ORDER BY bs.service_name) FILTER (WHERE bs.booking_id IS NOT NULL), '[]'),
		       COALESCE(SUM(bs.price), 0)::text
		FROM bookings b
		JOIN staff s ON s.id = b.staff_id
		LEFT JOIN booking_services bs ON bs.booking_id = b.id
		WHERE ` + whereSQL + `
		GROUP BY b.id, s.full_name
		ORDER BY b.start_time
		LIMIT ` + arg(f.Limit) + ` OFFSET ` + arg(f.Offset)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := []BookingSummary{}
	for rows.Next() {
		var item BookingSummary
		var start, end time.Time
		var namesRaw []byte
		if err := rows.Scan(&item.ID, &item.Code, &item.CustomerName, &item.CustomerPhone, &item.StaffName,
			&start, &end, &item.Status, &item.Source, &item.Note, &namesRaw, &item.TotalPrice); err != nil {
			return nil, 0, err
		}
		item.StartTime = start.Format(time.RFC3339)
		item.EndTime = end.Format(time.RFC3339)
		if err := json.Unmarshal(namesRaw, &item.ServiceNames); err != nil {
			return nil, 0, err
		}
		list = append(list, item)
	}
	return list, total, rows.Err()
}
