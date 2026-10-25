package settings

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"barbershop/internal/httpx"
)

// DayHours เวลาเปิด-ปิดต่อวัน — key ของ OpeningHours เป็น "0".."6" (0 = อาทิตย์, เทียบกับ Date.getDay())
type DayHours struct {
	Open   string `json:"open"`
	Close  string `json:"close"`
	Closed bool   `json:"closed"`
}

type StoreInfo struct {
	StoreName    string            `json:"storeName"`
	Tagline      *string           `json:"tagline"`
	Description  *string           `json:"description"`
	LogoURL      *string           `json:"logoUrl"`
	HeroImageURL *string           `json:"heroImageUrl"`
	Address      *string           `json:"address"`
	Phone        *string           `json:"phone"`
	Email        *string           `json:"email"`
	MapEmbedURL  *string           `json:"mapEmbedUrl"`
	SocialLinks  map[string]string `json:"socialLinks"`
	OpeningHours map[string]DayHours `json:"openingHours"`
	Timezone     string            `json:"timezone"`
	MaxAdvanceDays int             `json:"maxAdvanceDays"`
	CancellationCutoffMinutes int  `json:"cancellationCutoffMinutes"`
	AutoConfirm  bool              `json:"autoConfirm"`
}

type GalleryImage struct {
	ImageURL     string  `json:"imageUrl"`
	Caption      *string `json:"caption"`
	DisplayOrder int     `json:"displayOrder"`
}

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

func (h *Handler) RegisterPublicRoutes(api *gin.RouterGroup) {
	api.GET("/public/store", h.getStore)
	api.GET("/public/gallery", h.getGallery)
}

func (h *Handler) getStore(c *gin.Context) {
	var info StoreInfo
	info.SocialLinks = map[string]string{}
	info.OpeningHours = map[string]DayHours{}

	err := h.pool.QueryRow(c.Request.Context(), `
		SELECT store_name, tagline, description, logo_url, hero_image_url,
		       address, phone, email, map_embed_url, social_links, opening_hours, timezone,
		       max_advance_days, cancellation_cutoff_minutes, auto_confirm_bookings
		FROM store_settings WHERE id = 1`,
	).Scan(
		&info.StoreName, &info.Tagline, &info.Description, &info.LogoURL, &info.HeroImageURL,
		&info.Address, &info.Phone, &info.Email, &info.MapEmbedURL, &info.SocialLinks, &info.OpeningHours, &info.Timezone,
		&info.MaxAdvanceDays, &info.CancellationCutoffMinutes, &info.AutoConfirm,
	)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, info)
}

func (h *Handler) getGallery(c *gin.Context) {
	rows, err := h.pool.Query(c.Request.Context(), `
		SELECT image_url, caption, display_order FROM gallery_images
		WHERE is_active = true ORDER BY display_order, created_at`)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	defer rows.Close()

	list := []GalleryImage{}
	for rows.Next() {
		var img GalleryImage
		if err := rows.Scan(&img.ImageURL, &img.Caption, &img.DisplayOrder); err != nil {
			httpx.Err(c, err)
			return
		}
		list = append(list, img)
	}
	if err := rows.Err(); err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, list)
}
