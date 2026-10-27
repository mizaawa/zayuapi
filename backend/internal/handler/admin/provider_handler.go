package admin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type providerMetrics struct {
	Balance       *float64 `json:"balance"`
	TodaySpend    *float64 `json:"today_spend"`
	TotalRecharge *float64 `json:"total_recharge"`
	TotalSpend    *float64 `json:"total_spend"`
	Currency      string   `json:"currency"`
}

type providerRecord struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	SiteType        string          `json:"site_type"`
	BaseURL         string          `json:"base_url"`
	AuthMode        string          `json:"auth_mode"`
	Account         string          `json:"account,omitempty"`
	UserID          string          `json:"user_id,omitempty"`
	ExchangeRate    float64         `json:"exchange_rate"`
	Notes           string          `json:"notes,omitempty"`
	SecretEncrypted string          `json:"secret_encrypted"`
	Status          string          `json:"status"`
	Metrics         providerMetrics `json:"metrics"`
	UpdatedAt       *time.Time      `json:"updated_at,omitempty"`
	LastError       string          `json:"last_error,omitempty"`
}

type providerView struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	SiteType     string          `json:"site_type"`
	BaseURL      string          `json:"base_url"`
	AuthMode     string          `json:"auth_mode"`
	Account      string          `json:"account,omitempty"`
	UserID       string          `json:"user_id,omitempty"`
	ExchangeRate float64         `json:"exchange_rate"`
	Notes        string          `json:"notes,omitempty"`
	Status       string          `json:"status"`
	Metrics      providerMetrics `json:"metrics"`
	UpdatedAt    *time.Time      `json:"updated_at,omitempty"`
	LastError    string          `json:"last_error,omitempty"`
}

func (r providerRecord) view() providerView {
	mode := r.AuthMode
	if mode == "api_key" {
		mode = "token"
	}
	return providerView{r.ID, r.Name, r.SiteType, r.BaseURL, mode, r.Account,
		r.UserID, r.ExchangeRate, r.Notes, r.Status, r.Metrics, r.UpdatedAt, r.LastError}
}

type providerInput struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	SiteType          string  `json:"site_type"`
	BaseURL           string  `json:"base_url"`
	AuthMode          string  `json:"auth_mode"`
	Account           string  `json:"account"`
	Password          string  `json:"password"`
	AccessToken       string  `json:"access_token"`
	RefreshToken      string  `json:"refresh_token"`
	TokenType         string  `json:"token_type"`
	UserID            string  `json:"user_id"`
	SystemAccessToken string  `json:"system_access_token"`
	ExchangeRate      float64 `json:"exchange_rate"`
	Notes             string  `json:"notes"`
}

var providerWriteMu sync.Mutex

func newProviderID() (string, error) {
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}
	return hex.EncodeToString(id), nil
}

func (h *AccountHandler) providerRecords(c *gin.Context) ([]providerRecord, error) {
	var records []providerRecord
	err := h.providerSettings.LoadProviderRecords(c.Request.Context(), &records)
	return records, err
}

func validateProviderInput(input *providerInput, previous *providerRecord) (providerRecord, string, error) {
	if input == nil {
		return providerRecord{}, "", errors.New("missing provider")
	}
	base, err := normalizeProviderURL(input.BaseURL)
	if err != nil {
		return providerRecord{}, "", err
	}
	name := strings.TrimSpace(input.Name)
	if len(name) == 0 || len(name) > 120 || len(input.Notes) > 1000 || len(input.Account) > 320 {
		return providerRecord{}, "", errors.New("invalid provider name or notes")
	}
	if input.SiteType != "sub2api" && input.SiteType != "newapi" {
		return providerRecord{}, "", errors.New("unsupported provider type")
	}
	if input.AuthMode == "api_key" {
		input.AuthMode = "token"
	}
	if input.AuthMode != "password" && input.AuthMode != "token" {
		return providerRecord{}, "", errors.New("unsupported login mode")
	}
	if input.AuthMode == "password" && strings.TrimSpace(input.Account) == "" {
		return providerRecord{}, "", errors.New("login account is required")
	}
	userID := strings.TrimSpace(input.UserID)
	if input.SiteType == "newapi" && input.AuthMode == "token" {
		if userID == "" || len(userID) > 128 || strings.IndexFunc(userID, unicode.IsControl) >= 0 {
			return providerRecord{}, "", errors.New("New API user ID must be a nonempty identifier without control characters (up to 128 bytes)")
		}
	}
	if math.IsNaN(input.ExchangeRate) || math.IsInf(input.ExchangeRate, 0) || input.ExchangeRate < 0 || input.ExchangeRate > 1e6 {
		return providerRecord{}, "", errors.New("invalid exchange rate")
	}
	rate := input.ExchangeRate
	if rate == 0 {
		rate = 1
	}
	record := providerRecord{Name: name, SiteType: input.SiteType, BaseURL: base,
		AuthMode: input.AuthMode, Account: strings.TrimSpace(input.Account), ExchangeRate: rate,
		UserID: userID, Notes: strings.TrimSpace(input.Notes)}
	secret := input.Password
	if input.AuthMode == "token" {
		if input.SiteType == "sub2api" {
			if input.AccessToken != "" || input.RefreshToken != "" {
				secret, err = encodeProviderToken(input.AccessToken, input.RefreshToken, input.TokenType)
				if err != nil {
					return providerRecord{}, "", err
				}
			} else {
				secret = ""
			}
		} else {
			if input.SystemAccessToken != "" {
				if strings.HasPrefix(strings.ToLower(strings.TrimSpace(input.SystemAccessToken)), "sk-") {
					return providerRecord{}, "", errors.New("New API requires the user system access token, not an sk- model API key")
				}
				secret, err = encodeProviderToken(input.SystemAccessToken, "", "Bearer")
				if err != nil {
					return providerRecord{}, "", err
				}
			} else {
				secret = ""
			}
		}
	}
	if previous != nil {
		record.ID = previous.ID
		// Never send a saved credential to a different origin during an edit/probe.
		oldMode := previous.AuthMode
		if oldMode == "api_key" {
			oldMode = "token"
		}
		if secret == "" && base == previous.BaseURL && input.SiteType == previous.SiteType && input.AuthMode == oldMode && strings.TrimSpace(input.Account) == previous.Account && userID == previous.UserID {
			record.SecretEncrypted = previous.SecretEncrypted
		}
	}
	if len(secret) > 8192 {
		return providerRecord{}, "", errors.New("login credential is too long")
	}
	if secret == "" && record.SecretEncrypted == "" {
		return providerRecord{}, "", errors.New("login credential is required")
	}
	return record, secret, nil
}

func (h *AccountHandler) probeRecord(c *gin.Context, record *providerRecord, secret string) error {
	if secret == "" {
		if h.providerEncryptor == nil {
			return errors.New("provider encryption unavailable")
		}
		var err error
		secret, err = h.providerEncryptor.Decrypt(record.SecretEncrypted)
		if err != nil {
			return errors.New("stored provider login is unavailable")
		}
	}
	metrics, effectiveSecret, err := probeProvider(c.Request.Context(), *record, secret)
	if err != nil {
		return err
	}
	if effectiveSecret == "" {
		effectiveSecret = secret
	}
	if effectiveSecret != "" {
		if h.providerEncryptor == nil {
			return errors.New("provider encryption unavailable")
		}
		ciphertext, err := h.providerEncryptor.Encrypt(effectiveSecret)
		if err != nil {
			return errors.New("could not protect provider login")
		}
		record.SecretEncrypted = ciphertext
	}
	record.Metrics = metrics
	record.Status = "connected"
	record.LastError = ""
	now := time.Now().UTC()
	record.UpdatedAt = &now
	return nil
}

// Protect the submitted credential before contacting a remote site. A failed
// probe must still leave a usable, encrypted login in the saved record.
func (h *AccountHandler) protectProviderCredential(record *providerRecord, secret string) error {
	if h.providerEncryptor == nil {
		return errors.New("provider encryption unavailable")
	}
	if secret == "" {
		if record.SecretEncrypted == "" {
			return errors.New("provider login is unavailable")
		}
		return nil
	}
	ciphertext, err := h.providerEncryptor.Encrypt(secret)
	if err != nil {
		return errors.New("could not protect provider login")
	}
	record.SecretEncrypted = ciphertext
	return nil
}

// ListProviders returns ten records at a time and an overview for the whole list.
func (h *AccountHandler) ListProviders(c *gin.Context) {
	records, err := h.providerRecords(c)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to load providers")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	const size = 10
	query := strings.ToLower(strings.TrimSpace(c.Query("search")))
	filter := strings.TrimSpace(c.Query("status"))
	filtered := make([]providerView, 0, len(records))
	connected := 0
	var totalBalance float64
	for _, record := range records {
		if record.Status == "connected" {
			connected++
		}
		if record.Metrics.Balance != nil {
			totalBalance += *record.Metrics.Balance * record.ExchangeRate
		}
		if query != "" && !strings.Contains(strings.ToLower(record.Name+" "+record.BaseURL), query) {
			continue
		}
		if filter != "" && filter != record.Status {
			continue
		}
		filtered = append(filtered, record.view())
	}
	pages := (len(filtered) + size - 1) / size
	if pages == 0 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * size
	end := start + size
	if end > len(filtered) {
		end = len(filtered)
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, gin.H{
		"items": filtered[start:end], "total": len(filtered), "page": page, "page_size": size, "pages": pages,
		"overview": gin.H{"provider_count": len(records), "connected_count": connected, "upstream_total_balance": totalBalance},
	})
}

func (h *AccountHandler) ProbeProvider(c *gin.Context) {
	var input providerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid provider")
		return
	}
	var previous *providerRecord
	if input.ID != "" {
		records, err := h.providerRecords(c)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, "Could not load provider")
			return
		}
		for index := range records {
			if records[index].ID == input.ID {
				previous = &records[index]
				break
			}
		}
		if previous == nil {
			response.NotFound(c, "Provider not found")
			return
		}
	}
	record, secret, err := validateProviderInput(&input, previous)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.probeRecord(c, &record, secret); err != nil {
		response.Error(c, http.StatusBadGateway, providerPublicError(err))
		return
	}
	response.Success(c, record.view())
}

func (h *AccountHandler) CreateProvider(c *gin.Context) {
	var input providerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid provider")
		return
	}
	record, secret, err := validateProviderInput(&input, nil)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.protectProviderCredential(&record, secret); err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err := h.probeRecord(c, &record, secret); err != nil {
		record.Status = "disconnected"
		record.LastError = providerPublicError(err)
	}
	record.ID, err = newProviderID()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Could not create provider")
		return
	}
	providerWriteMu.Lock()
	defer providerWriteMu.Unlock()
	records, err := h.providerRecords(c)
	if err == nil {
		records = append([]providerRecord{record}, records...)
		err = h.providerSettings.SaveProviderRecords(c.Request.Context(), records)
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Could not save provider")
		return
	}
	response.Created(c, record.view())
}

func (h *AccountHandler) UpdateProvider(c *gin.Context) {
	var input providerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid provider")
		return
	}
	providerWriteMu.Lock()
	defer providerWriteMu.Unlock()
	records, err := h.providerRecords(c)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Could not load providers")
		return
	}
	for index := range records {
		if records[index].ID != c.Param("id") {
			continue
		}
		record, secret, err := validateProviderInput(&input, &records[index])
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		if err := h.protectProviderCredential(&record, secret); err != nil {
			response.Error(c, http.StatusServiceUnavailable, err.Error())
			return
		}
		if err := h.probeRecord(c, &record, secret); err != nil {
			record.Status = "disconnected"
			record.LastError = providerPublicError(err)
		}
		records[index] = record
		if err := h.providerSettings.SaveProviderRecords(c.Request.Context(), records); err != nil {
			response.Error(c, http.StatusInternalServerError, "Could not save provider")
			return
		}
		response.Success(c, record.view())
		return
	}
	response.NotFound(c, "Provider not found")
}

func (h *AccountHandler) RefreshProvider(c *gin.Context) {
	providerWriteMu.Lock()
	defer providerWriteMu.Unlock()
	records, err := h.providerRecords(c)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Could not load providers")
		return
	}
	for index := range records {
		if records[index].ID != c.Param("id") {
			continue
		}
		probeErr := h.probeRecord(c, &records[index], "")
		if probeErr != nil {
			records[index].Status = "disconnected"
			records[index].LastError = providerPublicError(probeErr)
		}
		if err := h.providerSettings.SaveProviderRecords(c.Request.Context(), records); err != nil {
			response.Error(c, http.StatusInternalServerError, "Could not save provider")
			return
		}
		if probeErr != nil {
			response.Error(c, http.StatusBadGateway, providerPublicError(probeErr))
			return
		}
		response.Success(c, records[index].view())
		return
	}
	response.NotFound(c, "Provider not found")
}

func (h *AccountHandler) DeleteProvider(c *gin.Context) {
	providerWriteMu.Lock()
	defer providerWriteMu.Unlock()
	records, err := h.providerRecords(c)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Could not load providers")
		return
	}
	for index := range records {
		if records[index].ID == c.Param("id") {
			records = append(records[:index], records[index+1:]...)
			if err := h.providerSettings.SaveProviderRecords(c.Request.Context(), records); err != nil {
				response.Error(c, http.StatusInternalServerError, "Could not delete provider")
				return
			}
			response.Success(c, gin.H{"deleted": true})
			return
		}
	}
	response.NotFound(c, "Provider not found")
}
