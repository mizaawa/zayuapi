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

func TestGetLeaderboardReturnsOptionalAvatarAndDisplayName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usageRepo := &leaderboardUsageRepoStub{ranking: []usagestats.UserSpendingRankingItem{
		{UserID: 1, Email: "alice@example.com", Username: "Alice", AvatarURL: " https://cdn.example.com/alice.png "},
		{UserID: 2, Email: "bob@example.com", Username: "Bob"},
		{UserID: 3, Email: "carol@example.com"},
		{UserID: 4, Email: "  charlotte@example.com  ", Username: " \t "},
		{UserID: 5, Email: "a@example.com"},
		{UserID: 6, Email: "unicode@example.com", Username: "\u7528\u6237\u5168\u540d"},
		{UserID: 7, Email: "fallback@example.com", Username: "  Display Name  "},
		{UserID: 8, Email: "fallback@example.com", Username: "chosen@name.example"},
		{UserID: 9, Email: " \t ", Username: " \t "},
		{UserID: 10, Email: "\u7528\u6237@example.com"},
		{UserID: 11, Email: "fallback@example.com", Username: "A"},
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
	wantNames := []string{
		"Alice", "Bob", "c***@example.com", "ch***@example.com", "a***@example.com",
		"\u7528\u6237\u5168\u540d", "Display Name", "chosen@name.example", "***", "\u7528***@example.com", "A",
	}
	require.Len(t, response.Data.Entries, len(wantNames))
	require.Equal(t, "https://cdn.example.com/alice.png", response.Data.Entries[0]["avatar_url"])
	require.NotContains(t, response.Data.Entries[1], "avatar_url")
	for i, entry := range response.Data.Entries {
		require.Equal(t, wantNames[i], entry["display_name"], "entry %d", i)
		require.NotContains(t, entry, "user_id")
		require.NotContains(t, entry, "email")
		require.NotContains(t, entry, "username")
	}
}
