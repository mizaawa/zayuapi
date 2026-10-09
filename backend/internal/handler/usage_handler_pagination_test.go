package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userUsagePaginationRepo struct {
	service.UsageLogRepository
	filters usagestats.UsageLogFilters
	called  bool
	rows    int
	total   int64
}

func (r *userUsagePaginationRepo) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	r.called, r.filters = true, filters
	return make([]service.UsageLog, r.rows), &pagination.PaginationResult{
		Total: r.total, Pages: int((r.total + int64(params.PageSize) - 1) / int64(params.PageSize)),
	}, nil
}

func TestUserUsageListSupportsFastPaginationWithoutChangingExportDefaults(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		rows  int
		total int64
		skip  bool
		exact bool
	}{
		{"default exact", "", 2, 50, false, true},
		{"explicit exact", "&exact_total=true", 2, 50, false, true},
		{"fast next page", "&exact_total=false", 2, 3, true, false},
		{"fast final page", "&exact_total=false", 1, 1, true, true},
		{"fast empty first page", "&exact_total=false", 0, 0, true, true},
		{"fast empty later page", "&exact_total=false&page=2", 0, 2, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			repo := &userUsagePaginationRepo{rows: tc.rows, total: tc.total}
			h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
			})
			router.GET("/usage", h.List)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage?page_size=2&user_id=99"+tc.query, nil))
			require.Equal(t, http.StatusOK, rec.Code)
			require.True(t, repo.called)
			require.EqualValues(t, 7, repo.filters.UserID)
			require.Equal(t, tc.skip, repo.filters.SkipTotal)
			var result struct {
				Data struct {
					Total        int64 `json:"total"`
					TotalIsExact *bool `json:"total_is_exact"`
					Pages        int   `json:"pages"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
			require.Equal(t, tc.total, result.Data.Total)
			if tc.skip {
				require.NotNil(t, result.Data.TotalIsExact)
				require.Equal(t, tc.exact, *result.Data.TotalIsExact)
			} else {
				require.Nil(t, result.Data.TotalIsExact)
			}
			require.GreaterOrEqual(t, result.Data.Pages, 1)
		})
	}
}

func TestUserUsageListRejectsInvalidExactTotalBeforeQuery(t *testing.T) {
	repo := &userUsageRepoCapture{}
	router := newUserUsageRequestTypeTestRouter(repo)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage?exact_total=invalid", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, repo.listFilters.UserID)
}
