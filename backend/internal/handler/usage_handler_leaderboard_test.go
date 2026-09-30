//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type leaderboardUsageRepoStub struct {
	service.UsageLogRepository
	ranking []usagestats.UserSpendingRankingItem
}

func (s *leaderboardUsageRepoStub) GetPublicUserSpendingRanking(context.Context, time.Time, time.Time, int) (*usagestats.UserSpendingRankingResponse, error) {
	return &usagestats.UserSpendingRankingResponse{Ranking: s.ranking}, nil
}

func (s *leaderboardUsageRepoStub) GetUserStatsAggregated(context.Context, int64, time.Time, time.Time) (*usagestats.UsageStats, error) {
	return &usagestats.UsageStats{}, nil
}

type leaderboardUserRepoStub struct {
	service.UserRepository
}

func (*leaderboardUserRepoStub) GetLeaderboardParticipation(context.Context, int64) (bool, error) {
	return true, nil
}

func (*leaderboardUserRepoStub) SetLeaderboardParticipation(context.Context, int64, bool) error {
	return nil
}

type leaderboardSettingRepoStub struct {
	service.SettingRepository
}

func (*leaderboardSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{service.SettingKeyLeaderboardEnabled: "true"}, nil
}

func TestGetLeaderboardReturnsOptionalAvatarAndMaskedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usageRepo := &leaderboardUsageRepoStub{ranking: []usagestats.UserSpendingRankingItem{
		{UserID: 1, Email: "alice@example.com", Username: "Alice", AvatarURL: " https://cdn.example.com/alice.png "},
		{UserID: 2, Email: "bob@example.com", Username: "Bob"},
	}}
	usageSvc := service.NewUsageService(usageRepo, &leaderboardUserRepoStub{}, nil, nil)
	settings := service.NewSettingService(&leaderboardSettingRepoStub{}, &config.Config{})
	h := NewUsageHandler(usageSvc, nil, nil, settings)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/usage/leaderboard", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})

	h.GetLeaderboard(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			Entries []map[string]any `json:"entries"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data.Entries, 2)
	require.Equal(t, "https://cdn.example.com/alice.png", response.Data.Entries[0]["avatar_url"])
	require.Equal(t, "A***e", response.Data.Entries[0]["display_name"])
	require.Equal(t, "B***b", response.Data.Entries[1]["display_name"])
	require.NotContains(t, response.Data.Entries[1], "avatar_url")
	for _, entry := range response.Data.Entries {
		require.NotContains(t, entry, "user_id")
		require.NotContains(t, entry, "email")
		require.NotContains(t, entry, "username")
	}
}
