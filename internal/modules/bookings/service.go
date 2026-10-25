package bookings

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"barbershop/internal/audit"
	"barbershop/internal/httpx"
)

const (
	maxServicesPerBooking = 5
	codeAlphabet          = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	codeLength            = 6
)

var phonePattern = regexp.MustCompile(`^0\d{8,9}$`)
var phoneIntlPattern = regexp.MustCompile(`^\+66\d{8,9}$`)

type Manager struct {
	pool *pgxpool.Pool
	repo *Repository
}

func NewManager(pool *pgxpool.Pool) *Manager {
	return &Manager{pool: pool, repo: NewRepository(pool)}
}

// Actor ข้อมูลผู้ทำรายการ (nil = ลูกค้าจากเว็บหน้าบ้าน)
type Actor struct {
	UserID  *string
	StaffID string
	Role    string
	IP      string
	IsAdmin bool
}

func conflictSlot() *httpx.AppError {
	return httpx.Conflict("ช่วงเวลานี้เพิ่งถูกจองไปแล้ว กรุณาเลือกเวลาอื่น")
}

// ---- availability ----

type availabilityData struct {
	cfg        *StoreConfig
	storeWin   *Interval
	staffSlots []StaffSlots
	duration   int
	freeAt     map[string][]StaffSlots
	slots      []string
	date       time.Time
	dayStart   time.Time
}

func (m *Manager) loadAvailability(ctx context.Context, dateStr string, serviceIDs []string, staffID *string, excludeBookingID string) (*availabilityData, error) {
	cfg, err := m.repo.GetStoreConfig(ctx)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.FixedZone(cfg.Timezone, 7*3600)
	}

	date, err := time.ParseInLocation("2006-01-02", dateStr, loc)
	if err != nil {
		return nil, httpx.BadRequest("รูปแบบวันที่ไม่ถูกต้อง (YYYY-MM-DD)")
	}

	// ห้ามย้อนหลัง + ไม่เกิน max_advance_days
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if date.Before(today) {
		return nil, httpx.BadRequest("ไม่สามารถจองย้อนหลังได้ กรุณาเลือกวันที่ตั้งแต่วันนี้เป็นต้นไป")
	}
	maxDate := today.AddDate(0, 0, cfg.MaxAdvanceDays)
	if date.After(maxDate) {
		return nil, httpx.BadRequest(fmt.Sprintf("จองล่วงหน้าได้ไม่เกิน %d วัน", cfg.MaxAdvanceDays))
	}

	// บริการ
	if len(serviceIDs) == 0 || len(serviceIDs) > maxServicesPerBooking {
		return nil, httpx.BadRequest(fmt.Sprintf("เลือกบริการได้ 1–%d รายการต่อการจอง", maxServicesPerBooking))
	}
	services, err := m.repo.GetServicesByIDs(ctx, serviceIDs)
	if err != nil {
		return nil, err
	}
	if len(services) != len(serviceIDs) {
		return nil, httpx.BadRequest("มีบริการบางรายการที่ไม่พบหรือไม่พร้อมให้บริการ")
	}
	for _, s := range services {
		if !s.IsActive {
			return nil, httpx.BadRequest(fmt.Sprintf("บริการ %q ปิดให้บริการอยู่ในขณะนี้", s.Name))
		}
	}
	duration := 0
	for _, s := range services {
		duration += s.DurationMinutes
	}

	// เวลาเปิดร้านของวันนั้น
	data := &availabilityData{cfg: cfg, date: date, dayStart: today}
	data.dayStart = date
	dayKey := fmt.Sprintf("%d", int(date.Weekday()))
	if hours, ok := cfg.OpeningHours[dayKey]; ok && !hours.Closed {
		start, err1 := ParseClock(hours.Open)
		end, err2 := ParseClock(hours.Close)
		if err1 == nil && err2 == nil && end > start {
			data.storeWin = &Interval{Start: start, End: end}
		}
	}
	if data.storeWin == nil {
		data.duration = duration
		data.freeAt = map[string][]StaffSlots{}
		data.slots = []string{}
		return data, nil // ร้านปิดวันนี้
	}

	closed, err := m.repo.IsClosedDate(ctx, date)
	if err != nil {
		return nil, err
	}
	if closed {
		data.duration = duration
		data.freeAt = map[string][]StaffSlots{}
		data.slots = []string{}
		return data, nil
	}

	// ช่าง — เก็บเป็น nil เมื่อไม่ระบุ (repo ใช้ nil = ไม่ filter)
	var staffIDs []string
	if staffID != nil {
		staffIDs = append(staffIDs, *staffID)
	}
	staffList, err := m.repo.ListBookableStaff(ctx, staffIDs)
	if err != nil {
		return nil, err
	}
	if staffID != nil && len(staffList) == 0 {
		return nil, httpx.BadRequest("ไม่พบช่างที่ระบุ หรือช่างคนนี้ไม่เปิดรับจอง")
	}
	ids := make([]string, 0, len(staffList))
	for _, s := range staffList {
		ids = append(ids, s.ID)
	}

	schedWin, err := m.repo.GetScheduleWindow(ctx, ids, int(date.Weekday()))
	if err != nil {
		return nil, err
	}
	timeOff, err := m.repo.GetTimeOffStaffIDs(ctx, ids, date)
	if err != nil {
		return nil, err
	}
	dayStartUTC := date.UTC()
	busy, bookedCounts, err := m.repo.GetBusyIntervals(ctx, ids, dayStartUTC, excludeBookingID)
	if err != nil {
		return nil, err
	}

	var minStart *int
	if date.Equal(today) {
		ms := now.Hour()*60 + now.Minute()
		minStart = &ms
	}

	for _, s := range staffList {
		if timeOff[s.ID] {
			continue
		}
		win := schedWin[s.ID]
		winCopy := win
		data.staffSlots = append(data.staffSlots, StaffSlots{
			StaffID:     s.ID,
			Window:      &winCopy,
			Busy:        busy[s.ID],
			BookedCount: bookedCounts[s.ID],
			Rank:        s.DisplayOrder,
		})
	}

	data.duration = duration
	data.slots, data.freeAt = ComputeAllSlots(EngineInput{
		StoreWindow: data.storeWin,
		Staff:       data.staffSlots,
		DurationMin: duration,
		SlotStepMin: 15,
		MinStart:    minStart,
	})
	return data, nil
}

func (m *Manager) Availability(ctx context.Context, dateStr string, serviceIDs []string, staffID *string) (*AvailabilityResponse, error) {
	data, err := m.loadAvailability(ctx, dateStr, serviceIDs, staffID, "")
	if err != nil {
		return nil, err
	}
	if data.slots == nil {
		data.slots = []string{}
	}
	return &AvailabilityResponse{
		Date:     dateStr,
		Timezone: data.cfg.Timezone,
		Slots:    data.slots,
	}, nil
}

// ---- create ----

func normalizePhone(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	s = strings.NewReplacer(" ", "", "-", "").Replace(s)
	if phonePattern.MatchString(s) {
		return s, true
	}
	if phoneIntlPattern.MatchString(s) {
		return "0" + s[3:], true
	}
	return "", false
}

func (m *Manager) Create(ctx context.Context, req CreateBookingRequest, actor *Actor) (*BookingDetail, error) {
	phone, ok := normalizePhone(req.CustomerPhone)
	if !ok {
		return nil, httpx.BadRequest("เบอร์โทรศัพท์ไม่ถูกต้อง (ใช้รูปแบบ 08X-XXX-XXXX หรือ 0XXXXXXXXX)")
	}

	var targetStaffID *string
	if req.StaffID != nil {
		id := *req.StaffID
		targetStaffID = &id
	}
	// staff จำกัดให้สร้างคิวได้เฉพาะกับตัวเอง
	if actor != nil && actor.Role == "staff" && actor.StaffID != "" {
		if req.StaffID != nil && *req.StaffID != actor.StaffID {
			return nil, httpx.Forbidden("พนักงานสร้างคิวได้เฉพาะกับช่างตัวเอง")
		}
		id := actor.StaffID
		targetStaffID = &id
	}

	data, err := m.loadAvailability(ctx, req.Date, req.ServiceIDs, targetStaffID, "")
	if err != nil {
		return nil, err
	}

	startMin, err := ParseClock(req.StartTime)
	if err != nil {
		return nil, httpx.BadRequest("รูปแบบเวลาไม่ถูกต้อง (HH:MM)")
	}
	slotKey := req.StartTime
	freeStaff, slotValid := data.freeAt[slotKey]
	if !slotValid {
		return nil, httpx.BadRequest("เวลาที่เลือกไม่พร้อมให้จอง กรุณาเลือกเวลาใหม่")
	}

	// กำหนดช่าง
	var chosen StaffSlots
	if targetStaffID != nil {
		found := false
		for _, s := range freeStaff {
			if s.StaffID == *targetStaffID {
				chosen = s
				found = true
				break
			}
		}
		if !found {
			return nil, conflictSlot()
		}
	} else {
		picked := PickStaff(freeStaff)
		if picked == nil {
			return nil, conflictSlot()
		}
		chosen = *picked
	}

	// สถานะเริ่มต้น
	status := "pending"
	if data.cfg.AutoConfirm {
		status = "confirmed"
	}
	if actor != nil && actor.IsAdmin && req.Status != nil && *req.Status != "" {
		if *req.Status == "confirmed" || *req.Status == "pending" {
			status = *req.Status
		}
	}

	services, err := m.repo.GetServicesByIDs(ctx, req.ServiceIDs)
	if err != nil {
		return nil, err
	}

	// data.date อยู่ใน timezone ของร้านอยู่แล้ว
	startTime := data.date.Add(time.Duration(startMin) * time.Minute)
	endTime := startTime.Add(time.Duration(data.duration) * time.Minute)

	var created *BookingRow
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		code, err := generateCode()
		if err != nil {
			return nil, err
		}
		created, lastErr = m.insertBooking(ctx, insertArgs{
			code: code, status: status, staffID: chosen.StaffID,
			start: startTime, end: endTime, req: req, phone: phone,
			services: services, actor: actor,
		})
		if lastErr == nil {
			break
		}
		var pgErr *pgconn.PgError
		if errors.As(lastErr, &pgErr) {
			if pgErr.Code == "23P01" { // exclusion_violation — ทับคิวช่างคนเดียวกัน
				return nil, conflictSlot()
			}
			if pgErr.Code == "23505" { // code ซ้ำ — สุ่มใหม่
				continue
			}
			if pgErr.Code == "40P01" { // deadlock จากการ insert พร้อมกันใน gist index — retry
				continue
			}
		}
		return nil, lastErr
	}
	if created == nil {
		return nil, lastErr
	}
	return m.GetDetail(ctx, created.ID)
}

type insertArgs struct {
	code     string
	status   string
	staffID  string
	start    time.Time
	end      time.Time
	req      CreateBookingRequest
	phone    string
	services []ServiceRow
	actor    *Actor
}

func (m *Manager) insertBooking(ctx context.Context, a insertArgs) (*BookingRow, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var b BookingRow
	source := "public"
	if a.actor != nil && a.actor.IsAdmin {
		source = "admin"
	}
	var createdBy *string
	if a.actor != nil {
		createdBy = a.actor.UserID
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO bookings (code, customer_name, customer_phone, note, staff_id, start_time, end_time, status, source, created_by)
		VALUES ($1, $2, $3, NULLIF($4,''), $5, $6, $7, $8, $9::booking_source, $10)
		RETURNING id, created_at`,
		a.code, a.req.CustomerName, a.phone, deref(a.req.Note), a.staffID, a.start, a.end, a.status, source, createdBy,
	).Scan(&b.ID, &b.CreatedAt)
	if err != nil {
		return nil, err
	}
	for _, s := range a.services {
		if _, err := tx.Exec(ctx, `
			INSERT INTO booking_services (booking_id, service_id, service_name, price, duration_minutes)
			VALUES ($1, $2, $3, $4::numeric, $5)`,
			b.ID, s.ID, s.Name, s.Price, s.DurationMinutes); err != nil {
			return nil, err
		}
	}
	var ip string
	if a.actor != nil {
		ip = a.actor.IP
	}
	audit.Log(ctx, tx, userIDPtr(a.actor), "booking.created", "booking", b.ID, map[string]any{
		"code": a.code, "staff_id": a.staffID, "start": a.start.Format(time.RFC3339), "status": a.status,
	}, ip)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	b.Code = a.code
	return &b, nil
}

// ---- detail / lookup ----

func (m *Manager) GetDetail(ctx context.Context, id string) (*BookingDetail, error) {
	b, err := m.repo.GetBookingByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ไม่พบการจองนี้")
	}
	if err != nil {
		return nil, err
	}
	return m.buildDetail(ctx, b)
}

func (m *Manager) GetByCode(ctx context.Context, code, phone string) (*BookingDetail, error) {
	b, err := m.repo.GetBookingByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ไม่พบการจองที่ตรงกับข้อมูลนี้")
	}
	if err != nil {
		return nil, err
	}
	normalized, ok := normalizePhone(phone)
	if !ok || normalized != b.CustomerPhone {
		return nil, httpx.NotFound("ไม่พบการจองที่ตรงกับข้อมูลนี้")
	}
	detail, err := m.buildDetail(ctx, b)
	if err != nil {
		return nil, err
	}
	cfg, err := m.repo.GetStoreConfig(ctx)
	if err == nil {
		cutoff := time.Now().Add(time.Duration(cfg.CancellationCutoffMin) * time.Minute)
		detail.CanCancel = (b.Status == "pending" || b.Status == "confirmed") && b.StartTime.After(cutoff)
	}
	return detail, nil
}

func (m *Manager) buildDetail(ctx context.Context, b *BookingRow) (*BookingDetail, error) {
	cfg, err := m.repo.GetStoreConfig(ctx)
	if err != nil {
		return nil, err
	}
	var staffName string
	var staffNickname *string
	if err := m.pool.QueryRow(ctx, `SELECT full_name, nickname FROM staff WHERE id = $1`, b.StaffID).
		Scan(&staffName, &staffNickname); err != nil {
		return nil, err
	}
	services, err := m.repo.GetBookingServices(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	total := 0
	totalCents := 0
	for _, s := range services {
		total += s.DurationMinutes
		totalCents += priceToCents(s.Price)
	}
	return &BookingDetail{
		ID: b.ID, Code: b.Code, CustomerName: b.CustomerName, CustomerPhone: b.CustomerPhone,
		Note: b.Note, StaffID: b.StaffID, StaffName: staffName, StaffNickname: staffNickname,
		StartTime: b.StartTime.Format(time.RFC3339), EndTime: b.EndTime.Format(time.RFC3339),
		Timezone: cfg.Timezone, Status: b.Status, Source: b.Source,
		Services: services, TotalPrice: centsToPrice(totalCents),
		TotalDurationMinutes: total, CancelledReason: b.CancelledReason,
		CreatedAt: b.CreatedAt.Format(time.RFC3339),
	}, nil
}

// ---- status / reschedule / cancel ----

var allowedTransitions = map[string]map[string]bool{
	"pending":   {"confirmed": true, "cancelled": true, "completed": true, "no_show": true},
	"confirmed": {"completed": true, "cancelled": true, "no_show": true},
	"completed": {},
	"cancelled": {},
	"no_show":   {},
}

func (m *Manager) UpdateStatus(ctx context.Context, id, newStatus string, actor *Actor) (*BookingDetail, error) {
	b, err := m.repo.GetBookingByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ไม่พบการจองนี้")
	}
	if err != nil {
		return nil, err
	}
	// staff แก้ได้เฉพาะคิวของตัวเอง และเฉพาะปิดคิว
	if actor != nil && actor.Role == "staff" {
		if actor.StaffID == "" || actor.StaffID != b.StaffID {
			return nil, httpx.Forbidden("คุณสามารถจัดการได้เฉพาะคิวของตัวเอง")
		}
		if newStatus != "completed" && newStatus != "no_show" {
			return nil, httpx.Forbidden("พนักงานสามารถบันทึกได้เฉพาะการเสร็จสิ้นหรือไม่มาตามนัด")
		}
	}
	if !allowedTransitions[b.Status][newStatus] {
		return nil, httpx.BadRequest(fmt.Sprintf("ไม่สามารถเปลี่ยนสถานะจาก %s เป็น %s ได้", statusLabel(b.Status), statusLabel(newStatus)))
	}
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE bookings SET status = $2, updated_at = now() WHERE id = $1`, id, newStatus); err != nil {
		return nil, err
	}
	audit.Log(ctx, tx, userIDPtr(actor), "booking.status_changed", "booking", id,
		map[string]any{"from": b.Status, "to": newStatus}, ipOf(actor))
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	slog.Info("booking status changed", "id", id, "from", b.Status, "to", newStatus)
	return m.GetDetail(ctx, id)
}

func (m *Manager) Reschedule(ctx context.Context, id string, req RescheduleRequest, actor *Actor) (*BookingDetail, error) {
	b, err := m.repo.GetBookingByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ไม่พบการจองนี้")
	}
	if err != nil {
		return nil, err
	}
	if b.Status != "pending" && b.Status != "confirmed" {
		return nil, httpx.BadRequest("ไม่สามารถเลื่อนเวลาการจองที่จบสถานะแล้ว")
	}

	services, err := m.repo.GetBookingServices(ctx, id)
	if err != nil {
		return nil, err
	}
	// หา service IDs จริงจาก snapshot เพื่อคำนวณ availability — ใช้ชื่อไม่ได้ ต้อง query
	rows, err := m.pool.Query(ctx, `SELECT service_id FROM booking_services WHERE booking_id = $1`, id)
	if err != nil {
		return nil, err
	}
	var serviceIDs []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			rows.Close()
			return nil, err
		}
		serviceIDs = append(serviceIDs, sid)
	}
	rows.Close()
	if len(services) == 0 || len(serviceIDs) == 0 {
		return nil, httpx.BadRequest("การจองนี้ไม่มีรายการบริการ ไม่สามารถเลื่อนเวลาอัตโนมัติ")
	}

	targetStaff := req.StaffID
	if targetStaff == nil {
		current := b.StaffID
		targetStaff = &current
	}

	data, err := m.loadAvailability(ctx, req.Date, serviceIDs, targetStaff, id)
	if err != nil {
		return nil, err
	}
	if _, ok := data.freeAt[req.StartTime]; !ok {
		return nil, httpx.BadRequest("เวลาใหม่ที่เลือกไม่พร้อมให้จอง กรุณาเลือกเวลาอื่น")
	}
	startMin, _ := ParseClock(req.StartTime)
	startTime := data.date.Add(time.Duration(startMin) * time.Minute)
	endTime := startTime.Add(time.Duration(data.duration) * time.Minute)

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE bookings SET staff_id = $2, start_time = $3, end_time = $4, updated_at = now()
		WHERE id = $1`, id, *targetStaff, startTime, endTime); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23P01" {
			return nil, conflictSlot()
		}
		return nil, err
	}
	audit.Log(ctx, tx, userIDPtr(actor), "booking.rescheduled", "booking", id, map[string]any{
		"from": b.StartTime.Format(time.RFC3339), "to": startTime.Format(time.RFC3339),
		"staff_from": b.StaffID, "staff_to": *targetStaff,
	}, ipOf(actor))
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return m.GetDetail(ctx, id)
}

func (m *Manager) Cancel(ctx context.Context, id, reason string, actor *Actor) (*BookingDetail, error) {
	b, err := m.repo.GetBookingByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ไม่พบการจองนี้")
	}
	if err != nil {
		return nil, err
	}
	if !allowedTransitions[b.Status]["cancelled"] {
		return nil, httpx.BadRequest("การจองนี้อยู่ในสถานะที่ยกเลิกไม่ได้")
	}
	if reason == "" {
		reason = "ยกเลิกโดยร้าน"
	}
	if err := m.applyCancel(ctx, id, reason, userIDPtr(actor), ipOf(actor)); err != nil {
		return nil, err
	}
	return m.GetDetail(ctx, id)
}

// CancelByCustomer ลูกค้ายกเลิกเอง — ยืนยันด้วยเบอร์โทร + เช็ค cutoff
func (m *Manager) CancelByCustomer(ctx context.Context, code, phone string) (*BookingDetail, error) {
	detail, err := m.GetByCode(ctx, code, phone)
	if err != nil {
		return nil, err
	}
	b, err := m.repo.GetBookingByID(ctx, detail.ID)
	if err != nil {
		return nil, err
	}
	cfg, err := m.repo.GetStoreConfig(ctx)
	if err != nil {
		return nil, err
	}
	if b.Status != "pending" && b.Status != "confirmed" {
		return nil, httpx.BadRequest("การจองนี้อยู่ในสถานะที่ยกเลิกไม่ได้")
	}
	cutoff := time.Now().Add(time.Duration(cfg.CancellationCutoffMin) * time.Minute)
	if !b.StartTime.After(cutoff) {
		return nil, httpx.BadRequest(
			fmt.Sprintf("ยกเลิกเองได้ก่อนเวลานัดอย่างน้อย %d นาที หากสายเกินกำหนดกรุณาโทรแจ้งร้าน", cfg.CancellationCutoffMin))
	}
	if err := m.applyCancel(ctx, b.ID, "ลูกค้ายกเลิกเองผ่านเว็บไซต์", nil, ""); err != nil {
		return nil, err
	}
	return m.GetDetail(ctx, b.ID)
}

func (m *Manager) applyCancel(ctx context.Context, id, reason string, userID *string, ip string) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE bookings SET status = 'cancelled', cancelled_reason = $2, updated_at = now() WHERE id = $1`,
		id, reason); err != nil {
		return err
	}
	audit.Log(ctx, tx, userID, "booking.cancelled", "booking", id, map[string]any{"reason": reason}, ip)
	return tx.Commit(ctx)
}

// ---- admin list ----

func (m *Manager) List(ctx context.Context, q ListBookingsQuery) ([]BookingSummary, int, error) {
	cfg, err := m.repo.GetStoreConfig(ctx)
	if err != nil {
		return nil, 0, err
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.UTC
	}
	f := BookingListFilters{Search: q.Search, StaffID: q.StaffID, Status: q.Status, Offset: (q.Page - 1) * q.Limit, Limit: q.Limit}
	if q.DateFrom != "" {
		if t, err := time.ParseInLocation("2006-01-02", q.DateFrom, loc); err == nil {
			f.From = &t
		}
	}
	if q.DateTo != "" {
		if t, err := time.ParseInLocation("2006-01-02", q.DateTo, loc); err == nil {
			end := t.AddDate(0, 0, 1)
			f.To = &end
		}
	}
	return m.repo.ListBookings(ctx, f)
}

// ---- helpers ----

func generateCode() (string, error) {
	out := make([]byte, codeLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			return "", err
		}
		out[i] = codeAlphabet[n.Int64()]
	}
	return "BK-" + string(out), nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func userIDPtr(a *Actor) *string {
	if a == nil {
		return nil
	}
	return a.UserID
}

func ipOf(a *Actor) string {
	if a == nil {
		return ""
	}
	return a.IP
}

func priceToCents(p string) int {
	var baht, cents int
	fmt.Sscanf(strings.TrimSpace(p), "%d.%d", &baht, &cents)
	// รองรับทั้ง "250" และ "250.00" / "250.5"
	if strings.Contains(p, ".") {
		frac := strings.SplitN(p, ".", 2)[1]
		for len(frac) < 2 {
			frac += "0"
		}
		fmt.Sscanf(frac, "%d", &cents)
	}
	return baht*100 + cents
}

func centsToPrice(c int) string {
	return fmt.Sprintf("%d.%02d", c/100, c%100)
}

func statusLabel(s string) string {
	switch s {
	case "pending":
		return "รอยืนยัน"
	case "confirmed":
		return "ยืนยันแล้ว"
	case "completed":
		return "เสร็จสิ้น"
	case "cancelled":
		return "ยกเลิก"
	case "no_show":
		return "ไม่มาตามนัด"
	}
	return s
}
