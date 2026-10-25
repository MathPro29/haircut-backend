package services

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"barbershop/internal/httpx"
)

type Handler struct {
	svc *Manager
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{svc: NewManager(NewRepository(pool))}
}

func (h *Handler) RegisterRoutes(
	api *gin.RouterGroup,
	authRequired gin.HandlerFunc,
	requireRole func(roles ...string) gin.HandlerFunc,
) {
	// Public — สำหรับเว็บไซต์หน้าบ้าน
	api.GET("/public/services", h.listPublic)

	// Admin
	admin := api.Group("/services")
	admin.GET("", authRequired, h.listAdmin)
	admin.POST("", authRequired, requireRole("owner", "manager"), h.create)
	admin.PATCH("/:id", authRequired, requireRole("owner", "manager"), h.update)
	admin.DELETE("/:id", authRequired, requireRole("owner", "manager"), h.remove)
}

func (h *Handler) listPublic(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context(), true)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, list)
}

func (h *Handler) listAdmin(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context(), false)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, list)
}

func (h *Handler) create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	created, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.Created(c, created)
}

func (h *Handler) update(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	updated, err := h.svc.Update(c.Request.Context(), id, req)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, updated)
}

func (h *Handler) remove(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Deactivate(c.Request.Context(), id); err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, gin.H{"message": "ปิดการใช้งานบริการเรียบร้อยแล้ว"})
}
