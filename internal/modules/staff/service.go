package staff

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"barbershop/internal/audit"
	"barbershop/internal/httpx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const staffColumns = `id, full_name, nickname, position, specialty, bio, phone, avatar_url, is_active, is_bookable, display_order`

func (r *Repository) List(ctx context.Context, includeInactive bool) ([]Staff, error) {
	q := `SELECT ` + staffColumns + ` FROM staff`
	if !includeInactive {
		q += ` WHERE is_active = true`
	}
	q += ` ORDER BY display_order, full_name`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Staff{}
	for rows.Next() {
		s, err := scanStaff(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *s)
	}
	return list, rows.Err()
}

func (r *Repository) GetByID(ctx context.Context, id string) (*Staff, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+staffColumns+` FROM staff WHERE id = $1`, id)
	s, err := scanStaff(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	return s, err
}

func scanStaff(row pgx.Row) (*Staff, error) {
	var s Staff
	err := row.Scan(&s.ID, &s.FullName, &s.Nickname, &s.Position, &s.Specialty, &s.Bio,
		&s.Phone, &s.AvatarURL, &s.IsActive, &s.IsBookable, &s.DisplayOrder)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ---- schedules ----

func (r *Repository) GetSchedule(ctx context.Context, staffID string) ([]ScheduleEntry, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT weekday, to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
		FROM staff_schedules WHERE staff_id = $1 ORDER BY weekday`, staffID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ScheduleEntry{}
	for rows.Next() {
		var e ScheduleEntry
		if err := rows.Scan(&e.Weekday, &e.Start, &e.End); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func (r *Repository) ReplaceSchedule(ctx context.Context, staffID string, entries []ScheduleEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM staff_schedules WHERE staff_id = $1`, staffID); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.Exec(ctx, `
			INSERT INTO staff_schedules (staff_id, weekday, start_time, end_time) VALUES ($1, $2, $3::time, $4::time)`,
			staffID, e.Weekday, e.Start, e.End); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ---- time off ----

func (r *Repository) ListTimeOff(ctx context.Context, staffID string) ([]TimeOff, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, staff_id, to_char(start_date, 'YYYY-MM-DD'), to_char(end_date, 'YYYY-MM-DD'), reason, type::text
		FROM staff_time_off WHERE staff_id = $1
		ORDER BY start_date DESC`, staffID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []TimeOff{}
	for rows.Next() {
		var t TimeOff
		if err := rows.Scan(&t.ID, &t.StaffID, &t.StartDate, &t.EndDate, &t.Reason, &t.Type); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

func (r *Repository) DeleteTimeOff(ctx context.Context, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM staff_time_off WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ---- attendance ----

const attendanceColumns = `a.id, a.staff_id, s.full_name, to_char(a.work_date, 'YYYY-MM-DD'), a.clock_in_at, a.clock_out_at, a.note`

func (r *Repository) ListAttendance(ctx context.Context, staffID string, from, to time.Time) ([]AttendanceRecord, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+attendanceColumns+` FROM attendance_records a
		JOIN staff s ON s.id = a.staff_id
		WHERE ($1::uuid IS NULL OR a.staff_id = $1)
		  AND a.work_date >= $2 AND a.work_date <= $3
		ORDER BY a.work_date DESC, a.clock_in_at DESC
		LIMIT 200`, nilIfEmpty(staffID), from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []AttendanceRecord{}
	for rows.Next() {
		a, err := scanAttendance(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *a)
	}
	return list, rows.Err()
}

func scanAttendance(row pgx.Row) (*AttendanceRecord, error) {
	var a AttendanceRecord
	var clockIn, clockOut *time.Time
	err := row.Scan(&a.ID, &a.StaffID, &a.StaffName, &a.WorkDate, &clockIn, &clockOut, &a.Note)
	if err != nil {
		return nil, err
	}
	if clockIn != nil {
		s := clockIn.Format(time.RFC3339)
		a.ClockIn = &s
	}
	if clockOut != nil {
		s := clockOut.Format(time.RFC3339)
		a.ClockOut = &s
	}
	return &a, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ================= Manager (business logic) =================

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

func notFoundStaff() *httpx.AppError {
	return httpx.NotFound("ไม่พบข้อมูลพนักงานนี้")
}

func (m *Manager) List(ctx context.Context, includeInactive bool) ([]Staff, error) {
	return m.repo.List(ctx, includeInactive)
}

func (m *Manager) Create(ctx context.Context, req CreateStaffRequest, actor Actor) (*Staff, error) {
	isActive, isBookable := true, true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	if req.IsBookable != nil {
		isBookable = *req.IsBookable
	}
	created := &Staff{
		FullName: req.FullName, Nickname: emptyToNil(req.Nickname), Position: emptyToNil(req.Position),
		Specialty: emptyToNil(req.Specialty), Bio: emptyToNil(req.Bio), Phone: emptyToNil(req.Phone),
		AvatarURL: emptyToNil(req.AvatarURL), IsActive: isActive, IsBookable: isBookable,
		DisplayOrder: req.DisplayOrder,
	}
	err := m.pool.QueryRow(ctx, `
		INSERT INTO staff (full_name, nickname, position, specialty, bio, phone, avatar_url, is_active, is_bookable, display_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		created.FullName, created.Nickname, created.Position, created.Specialty, created.Bio,
		created.Phone, created.AvatarURL, created.IsActive, created.IsBookable, created.DisplayOrder,
	).Scan(&created.ID)
	if err != nil {
		return nil, err
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "staff.created", "staff", created.ID, map[string]any{"name": created.FullName}, actor.IP)
	return created, nil
}

func (m *Manager) Update(ctx context.Context, id string, req UpdateStaffRequest, actor Actor) (*Staff, error) {
	cur, err := m.repo.GetByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFoundStaff()
	}
	if err != nil {
		return nil, err
	}
	if req.FullName != nil {
		cur.FullName = *req.FullName
	}
	if req.Nickname != nil {
		cur.Nickname = emptyToNil(req.Nickname)
	}
	if req.Position != nil {
		cur.Position = emptyToNil(req.Position)
	}
	if req.Specialty != nil {
		cur.Specialty = emptyToNil(req.Specialty)
	}
	if req.Bio != nil {
		cur.Bio = emptyToNil(req.Bio)
	}
	if req.Phone != nil {
		cur.Phone = emptyToNil(req.Phone)
	}
	if req.AvatarURL != nil {
		cur.AvatarURL = emptyToNil(req.AvatarURL)
	}
	if req.IsActive != nil {
		cur.IsActive = *req.IsActive
	}
	if req.IsBookable != nil {
		cur.IsBookable = *req.IsBookable
	}
	if req.DisplayOrder != nil {
		cur.DisplayOrder = *req.DisplayOrder
	}
	_, err = m.pool.Exec(ctx, `
		UPDATE staff SET full_name=$2, nickname=$3, position=$4, specialty=$5, bio=$6, phone=$7,
			avatar_url=$8, is_active=$9, is_bookable=$10, display_order=$11, updated_at=now()
		WHERE id=$1`,
		id, cur.FullName, cur.Nickname, cur.Position, cur.Specialty, cur.Bio, cur.Phone,
		cur.AvatarURL, cur.IsActive, cur.IsBookable, cur.DisplayOrder)
	if err != nil {
		return nil, err
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "staff.updated", "staff", id, nil, actor.IP)
	return m.repo.GetByID(ctx, id)
}

func (m *Manager) Deactivate(ctx context.Context, id string, actor Actor) error {
	cur, err := m.repo.GetByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFoundStaff()
	}
	if err != nil {
		return err
	}
	if _, err := m.pool.Exec(ctx, `UPDATE staff SET is_active=false, updated_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "staff.deactivated", "staff", id, map[string]any{"name": cur.FullName}, actor.IP)
	return nil
}

func (m *Manager) GetSchedule(ctx context.Context, staffID string) ([]ScheduleEntry, error) {
	if _, err := m.repo.GetByID(ctx, staffID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFoundStaff()
		}
		return nil, err
	}
	return m.repo.GetSchedule(ctx, staffID)
}

func (m *Manager) ReplaceSchedule(ctx context.Context, staffID string, req ScheduleRequest, actor Actor) ([]ScheduleEntry, error) {
	if _, err := m.repo.GetByID(ctx, staffID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFoundStaff()
		}
		return nil, err
	}
	seen := map[int]bool{}
	for _, e := range req.Entries {
		if seen[e.Weekday] {
			return nil, httpx.BadRequest("มีวันซ้ำในตารางงาน")
		}
		seen[e.Weekday] = true
		start, err1 := time.Parse("15:04", e.Start)
		end, err2 := time.Parse("15:04", e.End)
		if err1 != nil || err2 != nil || !end.After(start) {
			return nil, httpx.BadRequest(fmt.Sprintf("เวลาของวันที่ %d ไม่ถูกต้อง (เช่น 10:00–20:00)", e.Weekday))
		}
	}
	if err := m.repo.ReplaceSchedule(ctx, staffID, req.Entries); err != nil {
		return nil, err
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "staff.schedule_updated", "staff", staffID,
		map[string]any{"entries": len(req.Entries)}, actor.IP)
	return m.repo.GetSchedule(ctx, staffID)
}

func (m *Manager) ListTimeOff(ctx context.Context, staffID string) ([]TimeOff, error) {
	if _, err := m.repo.GetByID(ctx, staffID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFoundStaff()
		}
		return nil, err
	}
	return m.repo.ListTimeOff(ctx, staffID)
}

func (m *Manager) CreateTimeOff(ctx context.Context, staffID string, req CreateTimeOffRequest, actor Actor) (*TimeOff, error) {
	if _, err := m.repo.GetByID(ctx, staffID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, notFoundStaff()
		}
		return nil, err
	}
	start, err1 := time.Parse("2006-01-02", req.StartDate)
	end, err2 := time.Parse("2006-01-02", req.EndDate)
	if err1 != nil || err2 != nil || end.Before(start) {
		return nil, httpx.BadRequest("ช่วงวันที่ไม่ถูกต้อง (วันสิ้นสุดต้องไม่ก่อนวันเริ่ม)")
	}
	offType := req.Type
	if offType == "" {
		offType = "leave"
	}
	t := &TimeOff{}
	err := m.pool.QueryRow(ctx, `
		INSERT INTO staff_time_off (staff_id, start_date, end_date, reason, type, created_by)
		VALUES ($1, $2, $3, $4, $5::time_off_type, NULLIF($6,'')::uuid)
		RETURNING id, staff_id, to_char(start_date, 'YYYY-MM-DD'), to_char(end_date, 'YYYY-MM-DD'), reason, type::text`,
		staffID, start, end, req.Reason, offType, actor.UserID,
	).Scan(&t.ID, &t.StaffID, &t.StartDate, &t.EndDate, &t.Reason, &t.Type)
	if err != nil {
		return nil, err
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "staff.time_off_added", "staff", staffID,
		map[string]any{"from": req.StartDate, "to": req.EndDate, "reason": req.Reason}, actor.IP)
	return t, nil
}

func (m *Manager) DeleteTimeOff(ctx context.Context, id string, actor Actor) error {
	ok, err := m.repo.DeleteTimeOff(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.NotFound("ไม่พบรายการวันลานี้")
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "staff.time_off_deleted", "staff_time_off", id, nil, actor.IP)
	return nil
}

// ---- attendance ----

func (m *Manager) ClockIn(ctx context.Context, staffID string, req ClockRequest, actor Actor) (*AttendanceRecord, error) {
	// วันทำงานอิง timezone ของร้าน
	var tz string
	if err := m.pool.QueryRow(ctx, `SELECT timezone FROM store_settings WHERE id=1`).Scan(&tz); err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.FixedZone(tz, 7*3600)
	}
	now := time.Now().In(loc)
	workDate := now.Format("2006-01-02")

	// มี record วันนี้แล้ว?
	var exists bool
	if err := m.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM attendance_records WHERE staff_id=$1 AND work_date=$2)`,
		staffID, workDate).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, httpx.Conflict("วันนี้ลงเวลาเข้างานไปแล้ว")
	}
	var a AttendanceRecord
	var clockIn time.Time
	var clockOut *time.Time
	err = m.pool.QueryRow(ctx, `
		INSERT INTO attendance_records (staff_id, work_date, clock_in_at, note)
		VALUES ($1, $2, $3, NULLIF($4,''))
		RETURNING id, staff_id, to_char(work_date, 'YYYY-MM-DD'), clock_in_at, clock_out_at, note`,
		staffID, workDate, now, deref(req.Note),
	).Scan(&a.ID, &a.StaffID, &a.WorkDate, &clockIn, &clockOut, &a.Note)
	if err != nil {
		return nil, err
	}
	a.ClockIn = strPtr(clockIn.Format(time.RFC3339))
	if err := m.pool.QueryRow(ctx, `SELECT full_name FROM staff WHERE id=$1`, staffID).Scan(&a.StaffName); err != nil {
		return nil, err
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "attendance.clock_in", "staff", staffID, map[string]any{"date": workDate}, actor.IP)
	return &a, nil
}

func (m *Manager) ClockOut(ctx context.Context, staffID string, req ClockRequest, actor Actor) (*AttendanceRecord, error) {
	var tz string
	if err := m.pool.QueryRow(ctx, `SELECT timezone FROM store_settings WHERE id=1`).Scan(&tz); err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.FixedZone(tz, 7*3600)
	}
	now := time.Now().In(loc)
	workDate := now.Format("2006-01-02")

	tag, err := m.pool.Exec(ctx, `
		UPDATE attendance_records SET clock_out_at=$3, updated_at=now()
		WHERE staff_id=$1 AND work_date=$2 AND clock_out_at IS NULL`,
		staffID, workDate, now)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		// กรณี: ยังไม่เคยลงเข้างาน หรือลงออกงานไปแล้ว
		var clockedOut bool
		_ = m.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM attendance_records WHERE staff_id=$1 AND work_date=$2 AND clock_out_at IS NOT NULL)`,
			staffID, workDate).Scan(&clockedOut)
		if clockedOut {
			return nil, httpx.Conflict("วันนี้ลงเวลาออกงานไปแล้ว")
		}
		return nil, httpx.Conflict("ยังไม่ได้ลงเวลาเข้างาน — กดเข้างานก่อน")
	}
	audit.Log(ctx, m.pool, strPtr(actor.UserID), "attendance.clock_out", "staff", staffID, map[string]any{"date": workDate}, actor.IP)

	// ดึง record ล่าสุดกลับ
	var a AttendanceRecord
	var clockIn, clockOut time.Time
	err = m.pool.QueryRow(ctx, `
		SELECT a.id, a.staff_id, s.full_name, to_char(a.work_date, 'YYYY-MM-DD'), a.clock_in_at, a.clock_out_at, a.note
		FROM attendance_records a JOIN staff s ON s.id = a.staff_id
		WHERE a.staff_id=$1 AND a.work_date=$2`, staffID, workDate,
	).Scan(&a.ID, &a.StaffID, &a.StaffName, &a.WorkDate, &clockIn, &clockOut, &a.Note)
	if err != nil {
		return nil, err
	}
	a.ClockIn = strPtr(clockIn.Format(time.RFC3339))
	a.ClockOut = strPtr(clockOut.Format(time.RFC3339))
	return &a, nil
}

func (m *Manager) ListAttendance(ctx context.Context, staffID string, fromStr, toStr string) ([]AttendanceRecord, error) {
	var tz string
	if err := m.pool.QueryRow(ctx, `SELECT timezone FROM store_settings WHERE id=1`).Scan(&tz); err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.FixedZone(tz, 7*3600)
	}
	from, to := time.Now().In(loc).AddDate(0, 0, -30), time.Now().In(loc)
	if fromStr != "" {
		if t, err := time.ParseInLocation("2006-01-02", fromStr, loc); err == nil {
			from = t
		}
	}
	if toStr != "" {
		if t, err := time.ParseInLocation("2006-01-02", toStr, loc); err == nil {
			to = t
		}
	}
	return m.repo.ListAttendance(ctx, staffID, from, to)
}

func emptyToNil(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
