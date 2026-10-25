package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery คืน error JSON มาตรฐานแทนหน้า HTML เมื่อเกิด panic
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		slog.Error("panic recovered", "error", recovered, "stack", string(debug.Stack()))
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "internal", "message": "เกิดข้อผิดพลาดภายในระบบ กรุณาลองใหม่อีกครั้ง"},
		})
	})
}
