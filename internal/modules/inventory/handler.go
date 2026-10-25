package inventory

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"barbershop/internal/httpx"
	"barbershop/internal/middleware"
)

type Handler struct {
	mgr *Manager
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{mgr: NewManager(pool)}
}

func (h *Handler) RegisterRoutes(
	api *gin.RouterGroup,
	authRequired gin.HandlerFunc,
	requireRole func(roles ...string) gin.HandlerFunc,
) {
	g := api.Group("/inventory", authRequired)
	write := requireRole("owner", "manager")

	g.GET("/items", h.listItems)
	g.POST("/items", write, h.createItem)
	g.GET("/items/:id", h.getItem)
	g.PATCH("/items/:id", write, h.updateItem)
	g.DELETE("/items/:id", write, h.deactivateItem)
	g.GET("/items/:id/movements", h.listMovements)
	g.POST("/movements", write, h.applyMovement)
	g.GET("/barcode/:code", h.lookupByBarcode)

	g.GET("/categories", h.listCategories)
	g.POST("/categories", write, h.createCategory)
	g.PATCH("/categories/:id", write, h.updateCategory)
	g.DELETE("/categories/:id", write, h.deleteCategory)
}

// canSeeCost — ต้นทุนมองเห็นได้เฉพาะ owner/manager ตามข้อตกลง
func canSeeCost(c *gin.Context) bool {
	role := c.GetString(middleware.CtxRole)
	return role == "owner" || role == "manager"
}

func actorOf(c *gin.Context) Actor {
	return Actor{
		UserID: c.GetString(middleware.CtxUserID),
		Role:   c.GetString(middleware.CtxRole),
		IP:     c.ClientIP(),
	}
}

func (h *Handler) listItems(c *gin.Context) {
	q := ListItemsQuery{
		Query:           strings.TrimSpace(c.Query("q")),
		CategoryID:      c.Query("category_id"),
		LowStockOnly:    c.Query("low_stock") == "true",
		IncludeInactive: c.Query("include_inactive") == "true",
		Page:            parseIntDefault(c.Query("page"), 1),
		Limit:           parseIntDefault(c.Query("limit"), 20),
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 20
	}
	items, total, err := h.mgr.ListItems(c.Request.Context(), q)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	if !canSeeCost(c) {
		for i := range items {
			items[i].CostPrice = nil
		}
	}
	httpx.OKList(c, items, httpx.PageMeta{Page: q.Page, Limit: q.Limit, Total: total})
}

func (h *Handler) getItem(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	item, err := h.mgr.GetItem(c.Request.Context(), id)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	if !canSeeCost(c) {
		item.CostPrice = nil
	}
	httpx.OK(c, item)
}

func (h *Handler) createItem(c *gin.Context) {
	var req CreateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	item, err := h.mgr.CreateItem(c.Request.Context(), req, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.Created(c, item)
}

func (h *Handler) updateItem(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	var req UpdateItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	item, err := h.mgr.UpdateItem(c.Request.Context(), id, req, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, item)
}

func (h *Handler) deactivateItem(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.mgr.DeactivateItem(c.Request.Context(), id, actorOf(c)); err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, gin.H{"message": "ปิดการใช้งานสินค้าเรียบร้อยแล้ว"})
}

func (h *Handler) listMovements(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	page := parseIntDefault(c.Query("page"), 1)
	limit := parseIntDefault(c.Query("limit"), 10)
	list, total, err := h.mgr.ListMovements(c.Request.Context(), id, page, limit)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OKList(c, list, httpx.PageMeta{Page: page, Limit: limit, Total: total})
}

func (h *Handler) applyMovement(c *gin.Context) {
	var req MovementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	item, record, err := h.mgr.ApplyMovement(c.Request.Context(), req, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	if !canSeeCost(c) {
		item.CostPrice = nil
	}
	httpx.Created(c, gin.H{"item": item, "movement": record})
}

func (h *Handler) lookupByBarcode(c *gin.Context) {
	code := strings.TrimSpace(c.Param("code"))
	if code == "" {
		httpx.Err(c, httpx.BadRequest("ต้องระบุบาร์โค้ด"))
		return
	}
	item, err := h.mgr.GetByBarcode(c.Request.Context(), code)
	if err != nil {
		httpx.Err(c, httpx.NotFound("ไม่พบสินค้าที่ใช้บาร์โค้ดนี้ในระบบ"))
		return
	}
	if !canSeeCost(c) {
		item.CostPrice = nil
	}
	httpx.OK(c, item)
}

func (h *Handler) listCategories(c *gin.Context) {
	list, err := h.mgr.ListCategories(c.Request.Context())
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, list)
}

func (h *Handler) createCategory(c *gin.Context) {
	var req CategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	cat, err := h.mgr.CreateCategory(c.Request.Context(), req.Name, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.Created(c, cat)
}

func (h *Handler) updateCategory(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	var req CategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	cat, err := h.mgr.UpdateCategory(c.Request.Context(), id, req.Name, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, cat)
}

func (h *Handler) deleteCategory(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.mgr.DeleteCategory(c.Request.Context(), id, actorOf(c)); err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, gin.H{"message": "ลบหมวดหมู่เรียบร้อยแล้ว"})
}

func parseIntDefault(v string, def int) int {
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	return n
}
