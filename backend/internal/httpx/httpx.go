package httpx

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type Limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func NewLimiter() *Limiter { return &Limiter{hits: map[string][]time.Time{}} }

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Next()
	}
}

func RequestSizeLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		}
		c.Next()
	}
}

func (l *Limiter) Middleware(max int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP() + ":" + c.FullPath()
		now := time.Now()
		l.mu.Lock()
		kept := l.hits[key][:0]
		for _, hit := range l.hits[key] {
			if now.Sub(hit) < window {
				kept = append(kept, hit)
			}
		}
		if len(kept) >= max {
			l.hits[key] = kept
			l.mu.Unlock()
			Error(c, http.StatusTooManyRequests, "RATE_LIMITED", "Слишком много запросов")
			return
		}
		l.hits[key] = append(kept, now)
		l.mu.Unlock()
		c.Next()
	}
}

func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func Bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		Error(c, http.StatusBadRequest, "INVALID_JSON", "Некорректный JSON")
		return false
	}
	return true
}
