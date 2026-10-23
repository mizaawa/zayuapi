//go:build unit

package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// Exercise real HTTP handlers and stored configurations. A metadata-only update
// must not disclose a credential that was never included in that request.
func TestAdminPaymentProviderResponsesOmitSecrets(t *testing.T) {
	providers := []struct {
		key     string
		public  map[string]string
		secrets []string
	}{
		{payment.TypeEasyPay, map[string]string{"pid": "1000"}, []string{"pkey", "PKEY"}},
		{payment.TypeAlipay, map[string]string{"appId": "fixture-app"}, []string{"privateKey", "publicKey", "alipayPublicKey"}},
		{payment.TypeWxpay, map[string]string{"mchId": "fixture-merchant"}, []string{"privateKey", "apiV3Key", "publicKey"}},
		{payment.TypeStripe, map[string]string{"publishableKey": "pk_test_fixture"}, []string{"secretKey", "webhookSecret"}},
		{payment.TypeAirwallex, map[string]string{"clientId": "fixture-client"}, []string{"apiKey", "webhookSecret"}},
	}
	for _, provider := range providers {
		for _, operation := range []string{"create", "rename", "save-with-empty-secrets"} {
			t.Run(provider.key+"/"+operation, func(t *testing.T) {
				router, client, _ := newAdminPaymentProviderResponseFixture(t)
				ctx := context.Background()
				publicConfig := map[string]string{"notifyUrl": "https://site.example.com/payment/webhook"}
				storedConfig := make(map[string]string)
				for key, value := range provider.public {
					publicConfig[key] = value
				}
				for key, value := range publicConfig {
					storedConfig[key] = value
				}
				for _, key := range provider.secrets {
					storedConfig[key] = "fixture-" + provider.key + "-" + key + "-secret"
				}
				supportedType := provider.key
				if provider.key == payment.TypeEasyPay {
					supportedType = payment.TypeAlipay
				}
				method, path, wantStatus := http.MethodPost, "/api/v1/admin/payment/providers", http.StatusCreated
				body := map[string]any{
					"provider_key": provider.key, "name": "Fixture provider", "config": storedConfig,
					"supported_types": []string{supportedType}, "enabled": false,
					"payment_mode": "popup", "sort_order": 7, "limits": "{}",
					"refund_enabled": true, "allow_user_refund": true,
				}
				if operation != "create" {
					serializedConfig, err := json.Marshal(storedConfig)
					require.NoError(t, err)
					instance, err := client.PaymentProviderInstance.Create().SetProviderKey(provider.key).
						SetName("Original provider").SetConfig(string(serializedConfig)).SetSupportedTypes(supportedType).
						SetEnabled(false).SetPaymentMode("popup").SetSortOrder(7).SetLimits("{}").
						SetRefundEnabled(true).SetAllowUserRefund(true).Save(ctx)
					require.NoError(t, err)
					method, path, wantStatus = http.MethodPut, path+"/"+strconv.FormatInt(instance.ID, 10), http.StatusOK
					body = map[string]any{"name": "Fixture provider"}
					if operation == "save-with-empty-secrets" {
						patchConfig := map[string]string{"notifyUrl": "https://site.example.com/changed-webhook"}
						publicConfig["notifyUrl"] = patchConfig["notifyUrl"]
						storedConfig["notifyUrl"] = patchConfig["notifyUrl"]
						for _, key := range provider.secrets {
							patchConfig[key] = ""
						}
						body["config"] = patchConfig
					}
				}
				recorder := sendAdminPaymentProviderResponseRequest(t, router, method, path, body)
				require.Equal(t, wantStatus, recorder.Code, recorder.Body.String())
				exposed := false
				for _, key := range provider.secrets {
					exposed = exposed || strings.Contains(recorder.Body.String(), storedConfig[key])
				}
				t.Logf("method=%s provider=%s http_status=%d secret_exposed=%t", method, provider.key, recorder.Code, exposed)
				require.False(t, exposed, "provider response must not contain any stored secret")
				var envelope struct {
					Data service.ProviderInstanceResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
				result := envelope.Data
				require.Positive(t, result.ID)
				require.Equal(t, provider.key, result.ProviderKey)
				require.Equal(t, "Fixture provider", result.Name)
				require.Equal(t, publicConfig, result.Config)
				require.Equal(t, []string{supportedType}, result.SupportedTypes)
				require.Equal(t, "popup", result.PaymentMode)
				require.Equal(t, 7, result.SortOrder)
				require.Equal(t, "{}", result.Limits)
				require.False(t, result.Enabled)
				require.True(t, result.RefundEnabled)
				require.True(t, result.AllowUserRefund)
				persisted, err := client.PaymentProviderInstance.Get(ctx, result.ID)
				require.NoError(t, err)
				var actualConfig map[string]string
				require.NoError(t, json.Unmarshal([]byte(persisted.Config), &actualConfig))
				require.Equal(t, storedConfig, actualConfig, "secret filtering must not mutate stored credentials")
				t.Logf("persisted_secret_count=%d credentials_preserved=true", len(provider.secrets))
				list := sendAdminPaymentProviderResponseRequest(t, router, http.MethodGet, "/api/v1/admin/payment/providers", nil)
				require.Equal(t, http.StatusOK, list.Code)
				var listed struct {
					Data []service.ProviderInstanceResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(list.Body.Bytes(), &listed))
				require.Equal(t, []service.ProviderInstanceResponse{result}, listed.Data, "all provider responses must use the same DTO")
			})
		}
	}
}

func TestAdminPaymentProviderUpdateRedactsLegacyEncryptedConfig(t *testing.T) {
	router, client, encryptionKey := newAdminPaymentProviderResponseFixture(t)
	ctx := context.Background()
	config := `{"pid":"1000","pkey":"fixture-legacy-secret","notifyUrl":"https://site.example.com/webhook"}`
	encrypted, err := payment.Encrypt(config, encryptionKey)
	require.NoError(t, err)
	instance, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeEasyPay).
		SetName("Legacy provider").SetConfig(encrypted).SetSupportedTypes(payment.TypeAlipay).
		SetEnabled(false).Save(ctx)
	require.NoError(t, err)
	path := "/api/v1/admin/payment/providers/" + strconv.FormatInt(instance.ID, 10)
	result := sendAdminPaymentProviderResponseRequest(t, router, http.MethodPut, path, map[string]any{"name": "Renamed legacy provider"})
	require.Equal(t, http.StatusOK, result.Code)
	require.NotContains(t, result.Body.String(), "fixture-legacy-secret")
	require.NotContains(t, result.Body.String(), encrypted, "responses must not disclose legacy ciphertext either")
	var envelope struct {
		Data service.ProviderInstanceResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &envelope))
	require.Equal(t, map[string]string{"pid": "1000", "notifyUrl": "https://site.example.com/webhook"}, envelope.Data.Config)
	persisted, err := client.PaymentProviderInstance.Get(ctx, instance.ID)
	require.NoError(t, err)
	require.Equal(t, encrypted, persisted.Config, "metadata edits must preserve legacy encrypted storage")
	t.Logf("http_status=%d legacy_ciphertext_exposed=false credentials_preserved=true", result.Code)
}

func newAdminPaymentProviderResponseFixture(t *testing.T) (*gin.Engine, *dbent.Client, []byte) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := sql.Open("sqlite", fmt.Sprintf("file:admin_provider_response_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	_, err = database.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	encryptionKey := []byte("0123456789abcdef0123456789abcdef")
	configService := service.NewPaymentConfigService(client, nil, encryptionKey)
	paymentService := service.NewPaymentService(client, payment.NewRegistry(), payment.NewDefaultLoadBalancer(client, encryptionKey), nil, nil, configService, nil, nil, nil)
	handler := NewPaymentHandler(paymentService, configService)
	router := gin.New()
	router.GET("/api/v1/admin/payment/providers", handler.ListProviders)
	router.POST("/api/v1/admin/payment/providers", handler.CreateProvider)
	router.PUT("/api/v1/admin/payment/providers/:id", handler.UpdateProvider)
	return router, client, encryptionKey
}

func sendAdminPaymentProviderResponseRequest(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		require.NoError(t, err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}
