package settings

import (
	"encoding/json"
	"context"
	"regexp"

	"github.com/gin-gonic/gin"

	"barbershop/internal/audit"
	"barbershop/internal/httpx"
	"barbershop/internal/middleware"
)

var clockPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

type UpdateStoreRequest struct {
	StoreName    string             `json:"storeName" binding:"required,max=120"`
	Tagline      *string            `json:"tagline" binding:"omitempty,max=200"`
	Description  *string            `json:"description" binding:"omitempty,max=2000"`
	LogoURL      *string            `json:"logoUrl" binding:"omitempty,max=500"`
	HeroImageURL *string            `json:"heroImageUrl" binding:"omitempty,max=500"`
	Address      *string            `json:"address" binding:"omitempty,max=300"`
	Phone        *string            `json:"phone" binding:"omitempty,max=20"`
	Email        *string            `json:"email" binding:"omitempty,max=120"`
	MapEmbedURL  *string            `json:"mapEmbedUrl" binding:"omitempty,max=1000"`
	SocialLinks  map[string]string  `json:"socialLinks"`
	OpeningHours map[string]DayHours `json:"openingHours"`
	Timezone     string             `json:"timezone" binding:"omitempty,max=40"`
	MaxAdvanceDays int              `json:"maxAdvanceDays" binding:"min=1,max=365"`
	CancellationCutoffMinutes int   `json:"cancellationCutoffMinutes" binding:"min=0,max=1440"`
	AutoConfirm  bool               `json:"autoConfirm"`
}

type GalleryAdminItem struct {
	ID           string  `json:"id"`
	ImageURL     string  `json:"imageUrl"`
	Caption      *string `json:"caption"`
	DisplayOrder int     `json:"displayOrder"`
	IsActive     bool    `json:"isActive"`
}

type CreateGalleryRequest struct {
	ImageURL     string  `json:"imageUrl" binding:"required,max=1000"`
	Caption      *string `json:"caption" binding:"omitempty,max=200"`
	DisplayOrder int     `json:"displayOrder" binding:"min=0,max=9999"`
	IsActive     *bool   `json:"isActive"`
}

type UpdateGalleryRequest struct {
	ImageURL     *string `json:"imageUrl" binding:"omitempty,max=1000"`
	Caption      *string `json:"caption" binding:"omitempty,max=200"`
	DisplayOrder *int    `json:"displayOrder" binding:"omitempty,min=0,max=9999"`
	IsActive     *bool   `json:"isActive"`
}

func (h *Handler) RegisterAdminRoutes(
	api *gin.RouterGroup,
	authRequired gin.HandlerFunc,
	requireRole func(roles ...string) gin.HandlerFunc,
) {
	write := requireRole("owner", "manager")

	g := api.Group("/settings", authRequired)
	g.GET("/store", h.getStoreAdmin)
	g.PUT("/store", write, h.updateStore)

	gal := g.Group("/gallery")
	gal.GET("", h.listGalleryAdmin)
	gal.POST("", write, h.createGallery)
	gal.PATCH("/:id", write, h.updateGallery)
	gal.DELETE("/:id", write, h.deleteGallery)
}

func (h *Handler) getStoreAdmin(c *gin.Context) {
	h.getStore(c) // ชุดข้อมูลเดียวกับ public
}

func (h *Handler) updateStore(c *gin.Context) {
	var req UpdateStoreRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	// ตรวจเวลาเปิด-ปิดทุกวันที่ระบุ
	for day, hours := range req.OpeningHours {
		if day == "" {
			httpx.Err(c, httpx.BadRequest("รูปแบบวันในเวลาเปิด-ปิดไม่ถูกต้อง"))
			return
		}
		if hours.Closed {
			continue
		}
		if !clockPattern.MatchString(hours.Open) || !clockPattern.MatchString(hours.Close) || hours.Open >= hours.Close {
			httpx.Err(c, httpx.BadRequest("เวลาเปิด-ปิดไม่ถูกต้อง (เช่น 10:00 ถึง 20:00 และเวลาปิดต้องหลังเวลาเปิด)"))
			return
		}
	}

	userID := c.GetString(middleware.CtxUserID)
	ctx := c.Request.Context()
	if err := h.updateStoreRow(ctx, req, userID, c.ClientIP()); err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, gin.H{"message": "บันทึกการตั้งค่าเรียบร้อยแล้ว"})
}

func (h *Handler) updateStoreRow(ctx context.Context, req UpdateStoreRequest, userID, ip string) error {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE store_settings SET
			store_name=$1, tagline=$2, description=$3, logo_url=$4, hero_image_url=$5,
			address=$6, phone=$7, email=$8, map_embed_url=$9,
			social_links=$10::jsonb, opening_hours=$11::jsonb,
			timezone=COALESCE(NULLIF($12,''), timezone), max_advance_days=$13,
			cancellation_cutoff_minutes=$14, auto_confirm_bookings=$15,
			updated_by=NULLIF($16,'')::uuid, updated_at=now()
		WHERE id=1`,
		req.StoreName, req.Tagline, req.Description, req.LogoURL, req.HeroImageURL,
		req.Address, req.Phone, req.Email, req.MapEmbedURL,
		jsonOrNull(req.SocialLinks), jsonOrNull(req.OpeningHours),
		req.Timezone, req.MaxAdvanceDays, req.CancellationCutoffMinutes, req.AutoConfirm, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("ไม่พบข้อมูลร้าน")
	}
	audit.Log(ctx, tx, &userID, "settings.store_updated", "store_settings", "1", map[string]any{
		"store_name": req.StoreName, "auto_confirm": req.AutoConfirm,
	}, ip)
	return tx.Commit(ctx)
}

// ---- gallery admin ----

func (h *Handler) listGalleryAdmin(c *gin.Context) {
	rows, err := h.pool.Query(c.Request.Context(), `
		SELECT id, image_url, caption, display_order, is_active FROM gallery_images
		ORDER BY display_order, created_at`)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	defer rows.Close()
	list := []GalleryAdminItem{}
	for rows.Next() {
		var it GalleryAdminItem
		if err := rows.Scan(&it.ID, &it.ImageURL, &it.Caption, &it.DisplayOrder, &it.IsActive); err != nil {
			httpx.Err(c, err)
			return
		}
		list = append(list, it)
	}
	httpx.OK(c, list)
}

func (h *Handler) createGallery(c *gin.Context) {
	var req CreateGalleryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	var it GalleryAdminItem
	err := h.pool.QueryRow(c.Request.Context(), `
		INSERT INTO gallery_images (image_url, caption, display_order, is_active)
		VALUES ($1, $2, $3, $4)
		RETURNING id, image_url, caption, display_order, is_active`,
		req.ImageURL, req.Caption, req.DisplayOrder, isActive,
	).Scan(&it.ID, &it.ImageURL, &it.Caption, &it.DisplayOrder, &it.IsActive)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.Created(c, it)
}

func (h *Handler) updateGallery(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	var req UpdateGalleryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	cur := GalleryAdminItem{}
	err := h.pool.QueryRow(c.Request.Context(),
		`SELECT id, image_url, caption, display_order, is_active FROM gallery_images WHERE id=$1`, id,
	).Scan(&cur.ID, &cur.ImageURL, &cur.Caption, &cur.DisplayOrder, &cur.IsActive)
	if err != nil {
		httpx.Err(c, httpx.NotFound("ไม่พบรูปนี้"))
		return
	}
	if req.ImageURL != nil && *req.ImageURL != "" {
		cur.ImageURL = *req.ImageURL
	}
	if req.Caption != nil {
		if *req.Caption == "" {
			cur.Caption = nil
		} else {
			cur.Caption = req.Caption
		}
	}
	if req.DisplayOrder != nil {
		cur.DisplayOrder = *req.DisplayOrder
	}
	if req.IsActive != nil {
		cur.IsActive = *req.IsActive
	}
	_, err = h.pool.Exec(c.Request.Context(), `
		UPDATE gallery_images SET image_url=$2, caption=$3, display_order=$4, is_active=$5 WHERE id=$1`,
		id, cur.ImageURL, cur.Caption, cur.DisplayOrder, cur.IsActive)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, cur)
}

func (h *Handler) deleteGallery(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	tag, err := h.pool.Exec(c.Request.Context(), `DELETE FROM gallery_images WHERE id=$1`, id)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Err(c, httpx.NotFound("ไม่พบรูปนี้"))
		return
	}
	httpx.OK(c, gin.H{"message": "ลบรูปเรียบร้อยแล้ว"})
}

// jsonOrNull แปลง map เป็น JSON bytes ก่อนส่งให้ ::jsonb cast (pgx marshal map เองไม่ได้)
func jsonOrNull(v any) any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case map[string]string:
		if t == nil {
			return nil
		}
	case map[string]DayHours:
		if t == nil {
			return nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

