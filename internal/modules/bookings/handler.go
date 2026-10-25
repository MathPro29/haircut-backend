package bookings

import (
	"strconv"
	"strings"
	"time"

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
	// Public
	pub := api.Group("/public")
	pub.GET("/availability", h.availability)
	pub.POST("/bookings", middleware.RateLimit(5, 10*time.Minute), h.createPublic)
	pub.GET("/bookings/:code", h.lookup)
	pub.POST("/bookings/:code/cancel", h.cancelByCustomer)

	// Admin
	admin := api.Group("/bookings", authRequired)
	admin.GET("", h.list)
	admin.POST("", h.createAdmin)
	admin.GET("/:id", h.get)
	admin.POST("/:id/status", requireRole("owner", "manager", "staff"), h.updateStatus)
	admin.POST("/:id/reschedule", requireRole("owner", "manager"), h.reschedule)
	admin.POST("/:id/cancel", requireRole("owner", "manager"), h.cancel)
}

// ---- public ----

func (h *Handler) availability(c *gin.Context) {
	date := c.Query("date")
	serviceIDs := splitCSV(c.Query("service_ids"))
	var staffID *string
	if v := c.Query("staff_id"); v != "" {
		staffID = &v
	}
	if date == "" || len(serviceIDs) == 0 {
		httpx.Err(c, httpx.BadRequest("ต้องระบุ date และ service_ids"))
		return
	}
	res, err := h.mgr.Availability(c.Request.Context(), date, serviceIDs, staffID)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, res)
}

func (h *Handler) createPublic(c *gin.Context) {
	var req CreateBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	detail, err := h.mgr.Create(c.Request.Context(), req, &Actor{IP: c.ClientIP()})
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.Created(c, detail)
}

func (h *Handler) lookup(c *gin.Context) {
	phone := c.Query("phone")
	if phone == "" {
		httpx.Err(c, httpx.BadRequest("ต้องระบุเบอร์โทรศัพท์ที่ใช้จอง"))
		return
	}
	detail, err := h.mgr.GetByCode(c.Request.Context(), c.Param("code"), phone)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, detail)
}

func (h *Handler) cancelByCustomer(c *gin.Context) {
	var req CancelByCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	detail, err := h.mgr.CancelByCustomer(c.Request.Context(), c.Param("code"), req.Phone)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, detail)
}

// ---- admin ----

func (h *Handler) list(c *gin.Context) {
	q := ListBookingsQuery{
		Search:   strings.TrimSpace(c.Query("q")),
		StaffID:  c.Query("staff_id"),
		Status:   c.Query("status"),
		DateFrom: c.Query("date_from"),
		DateTo:   c.Query("date_to"),
		Page:     parseIntDefault(c.Query("page"), 1),
		Limit:    parseIntDefault(c.Query("limit"), 20),
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 20
	}
	// staff เห็นเฉพาะคิวของตัวเอง
	role := c.GetString(middleware.CtxRole)
	staffID := c.GetString(middleware.CtxStaffID)
	if role == "staff" && staffID != "" {
		q.StaffID = staffID
	}

	list, total, err := h.mgr.List(c.Request.Context(), q)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OKList(c, list, httpx.PageMeta{Page: q.Page, Limit: q.Limit, Total: total})
}

func (h *Handler) createAdmin(c *gin.Context) {
	var req CreateBookingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	actor := adminActor(c, true)
	detail, err := h.mgr.Create(c.Request.Context(), req, actor)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.Created(c, detail)
}

func (h *Handler) get(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	// staff ดูได้เฉพาะคิวตัวเอง
	if !h.canAccessOwnOnly(c, id) {
		return
	}
	detail, err := h.mgr.GetDetail(c.Request.Context(), id)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, detail)
}

func (h *Handler) updateStatus(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	var req UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	detail, err := h.mgr.UpdateStatus(c.Request.Context(), id, req.Status, adminActor(c, false))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, detail)
}

func (h *Handler) reschedule(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	var req RescheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	detail, err := h.mgr.Reschedule(c.Request.Context(), id, req, adminActor(c, false))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, detail)
}

func (h *Handler) cancel(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	var req CancelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	detail, err := h.mgr.Cancel(c.Request.Context(), id, req.Reason, adminActor(c, false))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, detail)
}

// canAccessOwnOnly staff ดูรายละเอียดได้เฉพาะคิวของตัวเอง (manager/owner เห็นทุกคิว)
func (h *Handler) canAccessOwnOnly(c *gin.Context, bookingID string) bool {
	role := c.GetString(middleware.CtxRole)
	staffID := c.GetString(middleware.CtxStaffID)
	if role != "staff" || staffID == "" {
		return true
	}
	detail, err := h.mgr.GetDetail(c.Request.Context(), bookingID)
	if err != nil {
		httpx.Err(c, err)
		return false
	}
	if detail.StaffID != staffID {
		httpx.Err(c, httpx.Forbidden("คุณสามารถดูได้เฉพาะคิวของตัวเอง"))
		return false
	}
	return true
}

func adminActor(c *gin.Context, withCreateSource bool) *Actor {
	role := c.GetString(middleware.CtxRole)
	userID := c.GetString(middleware.CtxUserID)
	return &Actor{
		UserID:  &userID,
		StaffID: c.GetString(middleware.CtxStaffID),
		Role:    role,
		IP:      c.ClientIP(),
		IsAdmin: true,
	}
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
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
