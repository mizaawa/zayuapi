//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestGetPublicUserSpendingRankingReturnsOnlyParticipatingUserAvatars(t *testing.T) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT, username TEXT, deleted_at TIMESTAMP);
		CREATE TABLE user_avatars (user_id INTEGER PRIMARY KEY, url TEXT);
		CREATE TABLE user_leaderboard_preferences (user_id INTEGER PRIMARY KEY, participating BOOLEAN);
		CREATE TABLE usage_logs (
			user_id INTEGER, actual_cost REAL, input_tokens INTEGER, output_tokens INTEGER,
			cache_creation_tokens INTEGER, cache_read_tokens INTEGER, created_at TIMESTAMP
		);
		INSERT INTO users (id, email, username) VALUES
			(1, 'alice@example.com', 'Alice'), (2, 'bob@example.com', 'Bob'),
			(3, 'private@example.com', 'Private'), (4, 'default@example.com', 'Default');
		INSERT INTO users (id, email, username, deleted_at) VALUES (5, 'deleted@example.com', 'Deleted', CURRENT_TIMESTAMP);
		INSERT INTO user_leaderboard_preferences VALUES (1, TRUE), (2, TRUE), (3, FALSE), (5, TRUE);
		INSERT INTO user_avatars VALUES (1, 'https://cdn.example.com/alice.png'),
			(3, 'https://cdn.example.com/private.png'), (4, 'https://cdn.example.com/default.png'),
			(5, 'https://cdn.example.com/deleted.png');
	`)
	require.NoError(t, err)
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		userID int64
		cost   float64
		tokens int64
	}{
		{1, 5, 20}, {1, 7, 40}, {2, 8, 20},
		{3, 100, 1000}, {4, 100, 1000}, {5, 100, 1000},
	} {
		_, err = db.Exec(`INSERT INTO usage_logs VALUES ($1, $2, $3, 0, 0, 0, $4)`, row.userID, row.cost, row.tokens, start.Add(time.Hour))
		require.NoError(t, err)
	}
	repo := &usageLogRepository{sql: db}

	result, err := repo.GetPublicUserSpendingRanking(context.Background(), start, start.Add(24*time.Hour), 25)
	require.NoError(t, err)
	require.Len(t, result.Ranking, 2)
	require.Equal(t, int64(1), result.Ranking[0].UserID)
	require.Equal(t, "https://cdn.example.com/alice.png", result.Ranking[0].AvatarURL)
	require.Equal(t, 12.0, result.Ranking[0].ActualCost)
	require.Equal(t, int64(2), result.Ranking[0].Requests)
	require.Equal(t, int64(60), result.Ranking[0].Tokens)
	require.Equal(t, int64(2), result.Ranking[1].UserID)
	require.Empty(t, result.Ranking[1].AvatarURL)
	require.Equal(t, 20.0, result.TotalActualCost)
	require.Equal(t, int64(3), result.TotalRequests)
	require.Equal(t, int64(80), result.TotalTokens)

	limited, err := repo.GetPublicUserSpendingRanking(context.Background(), start, start.Add(24*time.Hour), 1)
	require.NoError(t, err)
	require.Len(t, limited.Ranking, 1)
	require.Equal(t, result.Ranking[0], limited.Ranking[0])
	require.Equal(t, result.TotalActualCost, limited.TotalActualCost)
}
