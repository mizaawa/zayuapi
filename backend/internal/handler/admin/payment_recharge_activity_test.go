package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRechargeActivityRejectsInvalidFiltersBeforeQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{
		"/recharge-activity?page=0",
		"/recharge-activity?page=10001",
		"/recharge-activity?page=401",
		"/recharge-activity?status=unknown",
		"/recharge-activity?channel=other",
		"/recharge-activity?start_time=bad",
		"/recharge-activity?start_time=2026-10-27T00:00:00Z&end_time=2026-10-26T00:00:00Z",
	} {
		t.Run(path, func(t *testing.T) {
			router := gin.New()
			handler := &PaymentHandler{}
			router.GET("/recharge-activity", handler.RechargeActivity)
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
}
