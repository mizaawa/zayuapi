package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminUsagePaginationRepo struct {
	service.UsageLogRepository
	filters  usagestats.UsageLogFilters
	total    int64
	estimate bool
}

func (r *adminUsagePaginationRepo) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	r.filters = filters
	return make([]service.UsageLog, 2), &pagination.PaginationResult{
		Total: r.total, Page: params.Page, PageSize: params.PageSize,
		Pages: int((r.total + int64(params.PageSize) - 1) / int64(params.PageSize)), TotalIsEstimate: r.estimate,
	}, nil
}

func TestAdminUsageListReportsWhetherTheTotalIsCounted(t *testing.T) {
	counted, estimated := true, false
	for _, tc := range []struct {
		name     string
		query    string
		total    int64
		estimate bool
		exact    *bool
	}{
		{"skipped count with a next page", "", 3, true, &estimated},
		{"skipped count on the final page", "", 2, false, &counted},
		{"explicit exact count", "&exact_total=true", 50, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &adminUsagePaginationRepo{total: tc.total, estimate: tc.estimate}
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/admin/usage", NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil).List)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/usage?page_size=2"+tc.query, nil))
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, tc.exact == nil, repo.filters.ExactTotal)
			var result struct {
				Data struct {
					Total        int64 `json:"total"`
					TotalIsExact *bool `json:"total_is_exact"`
					Pages        int   `json:"pages"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
			require.Equal(t, tc.total, result.Data.Total)
			require.Equal(t, tc.exact, result.Data.TotalIsExact)
			require.GreaterOrEqual(t, result.Data.Pages, 1)
		})
	}
}
