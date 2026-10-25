package staff

import (
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

func actorOf(c *gin.Context) Actor {
	return Actor{
		UserID: c.GetString(middleware.CtxUserID),
		Role:   c.GetString(middleware.CtxRole),
		IP:     c.ClientIP(),
	}
}

func (h *Handler) RegisterRoutes(
	api *gin.RouterGroup,
	authRequired gin.HandlerFunc,
	requireRole func(roles ...string) gin.HandlerFunc,
) {
	write := requireRole("owner", "manager")

	g := api.Group("/staff", authRequired)
	g.GET("", h.list)
	g.POST("", write, h.create)
	g.PATCH("/:id", write, h.update)
	g.DELETE("/:id", write, h.deactivate)
	g.GET("/:id/schedule", h.getSchedule)
	g.PUT("/:id/schedule", write, h.replaceSchedule)
	g.GET("/:id/time-off", h.listTimeOff)
	g.POST("/:id/time-off", write, h.createTimeOff)
	g.DELETE("/:id/time-off/:offId", write, h.deleteTimeOff)

	att := api.Group("/attendance", authRequired)
	att.POST("/clock-in", h.clockIn)
	att.POST("/clock-out", h.clockOut)
	att.GET("", h.listAttendance)
}

func (h *Handler) list(c *gin.Context) {
	includeInactive := c.Query("include_inactive") == "true"
	list, err := h.mgr.List(c.Request.Context(), includeInactive)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, list)
}

func (h *Handler) create(c *gin.Context) {
	var req CreateStaffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	created, err := h.mgr.Create(c.Request.Context(), req, actorOf(c))
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
	var req UpdateStaffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	updated, err := h.mgr.Update(c.Request.Context(), id, req, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, updated)
}

func (h *Handler) deactivate(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.mgr.Deactivate(c.Request.Context(), id, actorOf(c)); err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, gin.H{"message": "ปิดการใช้งานพนักงานเรียบร้อยแล้ว"})
}

func (h *Handler) getSchedule(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	entries, err := h.mgr.GetSchedule(c.Request.Context(), id)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, entries)
}

func (h *Handler) replaceSchedule(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	var req ScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	entries, err := h.mgr.ReplaceSchedule(c.Request.Context(), id, req, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, entries)
}

func (h *Handler) listTimeOff(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	list, err := h.mgr.ListTimeOff(c.Request.Context(), id)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, list)
}

func (h *Handler) createTimeOff(c *gin.Context) {
	id, ok := httpx.UUIDParam(c, "id")
	if !ok {
		return
	}
	var req CreateTimeOffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	created, err := h.mgr.CreateTimeOff(c.Request.Context(), id, req, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.Created(c, created)
}

func (h *Handler) deleteTimeOff(c *gin.Context) {
	offID, ok := httpx.UUIDParam(c, "offId")
	if !ok {
		return
	}
	if err := h.mgr.DeleteTimeOff(c.Request.Context(), offID, actorOf(c)); err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, gin.H{"message": "ลบรายการวันลาเรียบร้อยแล้ว"})
}

func (h *Handler) clockIn(c *gin.Context) {
	staffID := c.GetString(middleware.CtxStaffID)
	if staffID == "" {
		httpx.Err(c, httpx.Forbidden("บัญชีของคุณยังไม่ได้เชื่อมกับข้อมูลพนักงาน — ติดต่อผู้ดูแล"))
		return
	}
	var req ClockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	record, err := h.mgr.ClockIn(c.Request.Context(), staffID, req, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.Created(c, record)
}

func (h *Handler) clockOut(c *gin.Context) {
	staffID := c.GetString(middleware.CtxStaffID)
	if staffID == "" {
		httpx.Err(c, httpx.Forbidden("บัญชีของคุณยังไม่ได้เชื่อมกับข้อมูลพนักงาน — ติดต่อผู้ดูแล"))
		return
	}
	var req ClockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	record, err := h.mgr.ClockOut(c.Request.Context(), staffID, req, actorOf(c))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, record)
}

func (h *Handler) listAttendance(c *gin.Context) {
	role := c.GetString(middleware.CtxRole)
	staffID := c.Query("staff_id")
	// staff เห็นได้เฉพาะของตัวเองเสมอ
	if role == "staff" {
		staffID = c.GetString(middleware.CtxStaffID)
		if staffID == "" {
			httpx.OK(c, []AttendanceRecord{})
			return
		}
	}
	list, err := h.mgr.ListAttendance(c.Request.Context(), staffID, c.Query("from"), c.Query("to"))
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, list)
}
