package admin

import (
	"context"
	"time"

	clientip "github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/uiaccess"
	"github.com/gin-gonic/gin"
)

// CheckUIAccess diagnoses the requesting administrator's actual network path.
// It accepts no target IP or URL and is registered only under the admin group.
func (h *SettingHandler) CheckUIAccess(c *gin.Context) {
	checkUICountry(c, uiaccess.DetectCountry)
}

func checkUICountry(c *gin.Context, detect func(context.Context, string, bool) (uiaccess.CountryLookupResult, error)) {
	started := time.Now()
	c.Header("Cache-Control", "private, no-store")
	visitorIP := clientip.GetSecurityClientIP(c, false)
	ctx, cancel := context.WithTimeout(c.Request.Context(), uiaccess.CountryLookupTimeout)
	defer cancel()
	result, err := detect(ctx, visitorIP, true)
	result.IP = visitorIP
	if err != nil {
		result.Country = ""
		if result.ErrorCode == "" {
			result.ErrorCode = "lookup_failed"
		}
	}
	response.Success(c, struct {
		uiaccess.CountryLookupResult
		ElapsedMS int64 `json:"elapsed_ms"`
	}{CountryLookupResult: result, ElapsedMS: time.Since(started).Milliseconds()})
}
