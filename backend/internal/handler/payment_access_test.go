//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestPaymentAccessMiddlewareRevokesExistingSessionAndKeepsAccessProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:payment_access_middleware?mode=memory&cache=shared")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	user, err := client.User.Create().SetEmail("middleware@example.com").SetPasswordHash("hash").SetExclusiveRechargeEnabled(true).Save(ctx)
	require.NoError(t, err)
	config := service.NewPaymentConfigService(client, repository.NewSettingRepository(client), nil)
	enabled := true
	require.NoError(t, config.UpdatePaymentConfig(ctx, service.UpdatePaymentConfigRequest{Enabled: &enabled, PaymentExclusiveEnabled: &enabled}))
	payments := service.NewPaymentService(client, payment.NewRegistry(), nil, nil, nil, config, nil, nil, nil)
	h := NewPaymentHandler(payments, config)
	router := gin.New()
	group := router.Group("/payment", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: user.ID})
	})
	group.GET("/access", h.GetPaymentAccess)
	group.Use(h.RequirePaymentAccess)
	group.GET("/config", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		return r
	}
	require.Equal(t, http.StatusNoContent, request("/payment/config").Code)
	require.NoError(t, client.User.UpdateOneID(user.ID).SetExclusiveRechargeEnabled(false).Exec(ctx))
	denied := request("/payment/config")
	unknown := request("/unknown")
	require.Equal(t, unknown.Code, denied.Code)
	require.Equal(t, unknown.Body.String(), denied.Body.String())
	require.Equal(t, unknown.Header().Get("Content-Type"), denied.Header().Get("Content-Type"))
	probe := request("/payment/access")
	require.Equal(t, http.StatusOK, probe.Code)
	require.Equal(t, "no-store", probe.Header().Get("Cache-Control"))
	var result struct {
		Data service.PaymentAccess `json:"data"`
	}
	require.NoError(t, json.Unmarshal(probe.Body.Bytes(), &result))
	require.False(t, result.Data.Enabled)
	require.False(t, result.Data.ExclusiveEnabled)
	require.False(t, result.Data.Allowed)
	enabled = false
	require.NoError(t, config.UpdatePaymentConfig(ctx, service.UpdatePaymentConfigRequest{PaymentExclusiveEnabled: &enabled}))
	require.Equal(t, http.StatusNoContent, request("/payment/config").Code)
	require.NoError(t, config.UpdatePaymentConfig(ctx, service.UpdatePaymentConfigRequest{Enabled: &enabled}))
	disabled := request("/payment/config")
	require.Equal(t, unknown.Code, disabled.Code)
	require.Equal(t, unknown.Body.String(), disabled.Body.String())
}
