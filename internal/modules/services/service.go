package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"barbershop/internal/httpx"
)

var pricePattern = regexp.MustCompile(`^\d{1,7}(\.\d{1,2})?$`)
var slugSeparator = regexp.MustCompile(`[^a-z0-9]+`)

type Manager struct {
	repo *Repository
}

func NewManager(repo *Repository) *Manager {
	return &Manager{repo: repo}
}

func (s *Manager) List(ctx context.Context, activeOnly bool) ([]Service, error) {
	return s.repo.List(ctx, activeOnly)
}

func (s *Manager) Create(ctx context.Context, req CreateRequest) (*Service, error) {
	if !pricePattern.MatchString(req.Price) {
		return nil, httpx.BadRequest("รูปแบบราคาไม่ถูกต้อง กรุณาระบุเป็นตัวเลข เช่น 250 หรือ 250.00")
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = generateSlug(req.Name)
	}
	exists, err := s.repo.SlugExists(ctx, slug, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, httpx.Conflict("มีบริการที่ใช้ slug นี้แล้ว กรุณาระบุ slug อื่น")
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	created := &Service{
		Name:            req.Name,
		Slug:            slug,
		Description:     emptyToNil(req.Description),
		Price:           req.Price,
		DurationMinutes: req.DurationMinutes,
		ImageURL:        emptyToNil(req.ImageURL),
		IsActive:        isActive,
		IsFeatured:      req.IsFeatured,
		DisplayOrder:    req.DisplayOrder,
	}
	if err := s.repo.Create(ctx, created); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Manager) Update(ctx context.Context, id string, req UpdateRequest) (*Service, error) {
	cur, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.NotFound("ไม่พบบริการนี้")
	}
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		cur.Name = *req.Name
	}
	if req.Slug != nil {
		slug := strings.TrimSpace(*req.Slug)
		if slug == "" {
			slug = generateSlug(cur.Name)
		}
		if slug != cur.Slug {
			exists, err := s.repo.SlugExists(ctx, slug, cur.ID)
			if err != nil {
				return nil, err
			}
			if exists {
				return nil, httpx.Conflict("มีบริการที่ใช้ slug นี้แล้ว กรุณาระบุ slug อื่น")
			}
		}
		cur.Slug = slug
	}
	if req.Description != nil {
		cur.Description = emptyToNil(req.Description)
	}
	if req.Price != nil {
		if !pricePattern.MatchString(*req.Price) {
			return nil, httpx.BadRequest("รูปแบบราคาไม่ถูกต้อง กรุณาระบุเป็นตัวเลข เช่น 250 หรือ 250.00")
		}
		cur.Price = *req.Price
	}
	if req.DurationMinutes != nil {
		cur.DurationMinutes = *req.DurationMinutes
	}
	if req.ImageURL != nil {
		cur.ImageURL = emptyToNil(req.ImageURL)
	}
	if req.IsActive != nil {
		cur.IsActive = *req.IsActive
	}
	if req.IsFeatured != nil {
		cur.IsFeatured = *req.IsFeatured
	}
	if req.DisplayOrder != nil {
		cur.DisplayOrder = *req.DisplayOrder
	}

	if err := s.repo.Update(ctx, cur); err != nil {
		return nil, err
	}
	return cur, nil
}

// Deactivate ปิดการแสดงบริการบนเว็บไซต์ (soft delete — ประวัติใน booking_services ยังครบ)
func (s *Manager) Deactivate(ctx context.Context, id string) error {
	cur, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.NotFound("ไม่พบบริการนี้")
	}
	if err != nil {
		return err
	}
	cur.IsActive = false
	return s.repo.Update(ctx, cur)
}

func emptyToNil(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}

func generateSlug(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = slugSeparator.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 140 {
		slug = slug[:140]
	}
	if slug == "" {
		// ชื่อที่ไม่มีอักษรละติน (เช่น ภาษาไทยล้วน) — ใช้รหัสสุ่มแทน
		b := make([]byte, 4)
		if _, err := rand.Read(b); err == nil {
			slug = "service-" + hex.EncodeToString(b)
		}
	}
	return slug
}
