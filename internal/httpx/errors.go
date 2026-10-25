package httpx

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

// AppError เป็น error มาตรฐานของระบบ — handler โยน error นี้เพื่อคืน status/message ที่เหมาะสม
type AppError struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

func (e *AppError) Error() string { return e.Message }

func New(status int, code, message string) *AppError {
	return &AppError{Status: status, Code: code, Message: message}
}

func BadRequest(message string) *AppError {
	return New(http.StatusBadRequest, "bad_request", message)
}

func Unauthorized(message ...string) *AppError {
	return New(http.StatusUnauthorized, "unauthorized", withDefault(message, "กรุณาเข้าสู่ระบบ"))
}

func Forbidden(message ...string) *AppError {
	return New(http.StatusForbidden, "forbidden", withDefault(message, "คุณไม่มีสิทธิ์ใช้งานส่วนนี้"))
}

func NotFound(message ...string) *AppError {
	return New(http.StatusNotFound, "not_found", withDefault(message, "ไม่พบข้อมูล"))
}

func Conflict(message string) *AppError {
	return New(http.StatusConflict, "conflict", message)
}

func Internal(message ...string) *AppError {
	return New(http.StatusInternalServerError, "internal", withDefault(message, "เกิดข้อผิดพลาดภายในระบบ กรุณาลองใหม่อีกครั้ง"))
}

func withDefault(msg []string, fallback string) string {
	if len(msg) > 0 {
		return msg[0]
	}
	return fallback
}

// Err แปลง error เป็น JSON response มาตรฐาน — error ที่ไม่ใช่ AppError ถือว่าเป็น internal
func Err(c *gin.Context, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		c.AbortWithStatusJSON(appErr.Status, gin.H{"error": appErr})
		return
	}
	slog.Error("unexpected error", "path", c.FullPath(), "error", err)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": Internal(),
	})
}

// BindError แปลงความผิดพลาดจากการ bind/validate request เป็นรายละเอียดราย field
func BindError(c *gin.Context, err error) {
	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		details := make(map[string]string, len(verrs))
		for _, fe := range verrs {
			details[fe.Field()] = fieldMessage(fe)
		}
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
			"error": AppError{
				Status:  http.StatusUnprocessableEntity,
				Code:    "validation_error",
				Message: "ข้อมูลไม่ถูกต้อง กรุณาตรวจสอบอีกครั้ง",
				Details: details,
			},
		})
		return
	}
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": BadRequest("รูปแบบคำขอไม่ถูกต้อง")})
}

func fieldMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "จำเป็นต้องกรอกข้อมูลนี้"
	case "min":
		return fmt.Sprintf("ต้องมีความยาวอย่างน้อย %s ตัวอักษร", fe.Param())
	case "max":
		return fmt.Sprintf("ต้องมีความยาวไม่เกิน %s ตัวอักษร", fe.Param())
	default:
		return fmt.Sprintf("ข้อมูลไม่ถูกต้อง (%s)", fe.Tag())
	}
}

// UUIDParam อ่านและตรวจ path parameter ว่าเป็น UUID — ตอบ error ให้เองหากไม่ถูกต้อง
func UUIDParam(c *gin.Context, name string) (string, bool) {
	raw := c.Param(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		Err(c, BadRequest("รูปแบบรหัสอ้างอิงไม่ถูกต้อง"))
		return "", false
	}
	return id.String(), true
}
