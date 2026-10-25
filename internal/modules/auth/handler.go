package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"barbershop/internal/httpx"
	"barbershop/internal/middleware"
	"barbershop/internal/token"
)

const refreshCookieName = "refresh_token"
const refreshCookiePath = "/api/v1/auth"

type Handler struct {
	svc          *Service
	tokens       *token.Manager
	secureCookie bool
	refreshTTL   time.Duration
}

func NewHandler(pool *pgxpool.Pool, tokens *token.Manager, secureCookie bool, refreshTTL time.Duration) *Handler {
	return &Handler{
		svc:          NewService(NewRepository(pool), tokens),
		tokens:       tokens,
		secureCookie: secureCookie,
		refreshTTL:   refreshTTL,
	}
}

func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, authRequired gin.HandlerFunc) {
	g := rg.Group("/auth")
	g.POST("/login", h.Login)
	g.POST("/refresh", h.Refresh)
	g.POST("/logout", h.Logout)
	g.GET("/me", authRequired, h.Me)
}

func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BindError(c, err)
		return
	}
	session, err := h.svc.Login(c.Request.Context(), req)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	h.setRefreshCookie(c, session.RefreshToken)
	httpx.OK(c, gin.H{"accessToken": session.AccessToken, "user": session.User})
}

func (h *Handler) Refresh(c *gin.Context) {
	raw, err := c.Cookie(refreshCookieName)
	if err != nil {
		httpx.Err(c, httpx.Unauthorized("ไม่พบเซสชัน กรุณาเข้าสู่ระบบอีกครั้ง"))
		return
	}
	claims, err := h.tokens.Parse(raw, token.TypeRefresh)
	if err != nil {
		httpx.Err(c, httpx.Unauthorized("เซสชันหมดอายุ กรุณาเข้าสู่ระบบอีกครั้ง"))
		return
	}
	session, err := h.svc.Refresh(c.Request.Context(), claims.Subject)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	h.setRefreshCookie(c, session.RefreshToken)
	httpx.OK(c, gin.H{"accessToken": session.AccessToken, "user": session.User})
}

func (h *Handler) Logout(c *gin.Context) {
	h.clearRefreshCookie(c)
	httpx.OK(c, gin.H{"message": "ออกจากระบบเรียบร้อยแล้ว"})
}

func (h *Handler) Me(c *gin.Context) {
	userID := c.GetString(middleware.CtxUserID)
	user, err := h.svc.GetUser(c.Request.Context(), userID)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	httpx.OK(c, user)
}

func (h *Handler) setRefreshCookie(c *gin.Context, value string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, value, int(h.refreshTTL.Seconds()), refreshCookiePath, "", h.secureCookie, true)
}

func (h *Handler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, "", -1, refreshCookiePath, "", h.secureCookie, true)
}
