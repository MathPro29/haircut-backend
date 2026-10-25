package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"barbershop/internal/httpx"
	"barbershop/internal/token"
)

const (
	CtxUserID = "userID"
	CtxRole   = "role"
	CtxStaffID = "staffID"
)

// AuthRequired ตรวจสอบ Bearer access token และเก็บ userID/role ลง context
func AuthRequired(tm *token.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			httpx.Err(c, httpx.Unauthorized())
			return
		}
		claims, err := tm.Parse(strings.TrimPrefix(header, "Bearer "), token.TypeAccess)
		if err != nil {
			httpx.Err(c, httpx.Unauthorized("เซสชันหมดอายุ กรุณาเข้าสู่ระบบอีกครั้ง"))
			return
		}
		c.Set(CtxUserID, claims.Subject)
		c.Set(CtxRole, claims.Role)
		c.Set(CtxStaffID, claims.StaffID)
		c.Next()
	}
}

// RequireRole จำกัด endpoint ตามบทบาท (ตรวจที่ backend เสมอ ไม่ใช่แค่ซ่อนเมนู)
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetString(CtxRole)
		for _, r := range roles {
			if r == role {
				c.Next()
				return
			}
		}
		httpx.Err(c, httpx.Forbidden())
	}
}
