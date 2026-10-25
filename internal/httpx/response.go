package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// PageMeta ใช้กับ endpoint ที่รองรับ pagination
type PageMeta struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

func OKList(c *gin.Context, data any, meta PageMeta) {
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": meta})
}
