package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"barbershop/internal/httpx"
)

type ipWindow struct {
	hits []time.Time
}

// RateLimit จำกัดจำนวน request ต่อ IP แบบ sliding window (เพียงพอสำหรับ single instance)
func RateLimit(maxEvents int, per time.Duration) gin.HandlerFunc {
	var mu sync.Mutex
	visitors := make(map[string]*ipWindow)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		mu.Lock()
		w, ok := visitors[ip]
		if !ok {
			w = &ipWindow{}
			visitors[ip] = w
		}
		// ตัด hits เก่าออก
		fresh := w.hits[:0]
		for _, t := range w.hits {
			if now.Sub(t) < per {
				fresh = append(fresh, t)
			}
		}
		if len(fresh) >= maxEvents {
			mu.Unlock()
			httpx.Err(c, httpx.New(http.StatusTooManyRequests, "rate_limited",
				"คุณส่งคำขอถี่เกินไป กรุณารอสักครู่แล้วลองใหม่อีกครั้ง"))
			return
		}
		w.hits = append(fresh, now)
		mu.Unlock()

		// ล้าง IP ที่ไม่ได้ใช้งานนาน ๆ ป้องกัน memory โต
		if len(visitors) > 10_000 {
			go func() {
				mu.Lock()
				defer mu.Unlock()
				for key, v := range visitors {
					if len(v.hits) == 0 || now.Sub(v.hits[len(v.hits)-1]) > per*4 {
						delete(visitors, key)
					}
				}
			}()
			slog.Debug("rate limit visitor map cleaned")
		}

		c.Next()
	}
}
