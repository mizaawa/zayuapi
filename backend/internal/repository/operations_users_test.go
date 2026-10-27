package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOperationsUsersTopSpendAndSelectedDetail(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newUsageLogRepositoryWithSQL(nil, db)
	start := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)

	mock.ExpectQuery(`SELECT u\.id, u\.email,.*FROM users u.*LEFT JOIN usage_logs ul.*u\.role = 'user'.*ORDER BY user_cost DESC.*LIMIT CASE WHEN \$3 = '' THEN 25 ELSE 100 END`).
		WithArgs(start, end, "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "user_cost", "upstream_cost"}).
			AddRow(12, "first@example.test", 42.0, 20.0).
			AddRow(13, "second@example.test", 30.0, 25.0))
	data, err := repo.GetOperationsUsersData(context.Background(), start, end, "", 0)
	require.NoError(t, err)
	require.Len(t, data.Users, 2)
	require.Equal(t, int64(12), data.Users[0].UserID)
	require.Equal(t, 42.0, data.Users[0].UserCost)
	require.Nil(t, data.Detail)
	require.NoError(t, mock.ExpectationsWereMet())

	mock.ExpectQuery(`SELECT u\.id, u\.email,.*FROM users u.*POSITION\(LOWER\(\$3\) IN LOWER\(u\.email\)\).*LIMIT CASE WHEN \$3 = '' THEN 25 ELSE 100 END`).
		WithArgs(start, end, "first@").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "user_cost", "upstream_cost"}).
			AddRow(12, "first@example.test", 42.0, 20.0))
	mock.ExpectQuery(`SELECT COALESCE\(\(SELECT email FROM users WHERE id = \$1 AND role = 'user' AND deleted_at IS NULL\), ''\)`).
		WithArgs(int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("first@example.test"))
	mock.ExpectQuery(`SELECT COALESCE\(ul\.group_id, 0\).*FROM usage_logs ul LEFT JOIN groups g.*GROUP BY 1, 2, 3, 4`).
		WithArgs(start, end, int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "group_name", "platform", "rate_multiplier", "user_cost", "upstream_cost"}).
			AddRow(2, "OpenAI", "openai", 1.2, 30.0, 15.0))
	mock.ExpectQuery(`SELECT COALESCE\(NULLIF\(TRIM\(ul\.requested_model\).*STRING_AGG.*FROM usage_logs ul LEFT JOIN groups g.*GROUP BY 1 ORDER BY user_cost DESC`).
		WithArgs(start, end, int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"model", "user_cost", "upstream_cost", "platforms"}).
			AddRow("gpt-5", 30.0, 15.0, "anthropic,openai"))
	data, err = repo.GetOperationsUsersData(context.Background(), start, end, "first@", 12)
	require.NoError(t, err)
	require.Len(t, data.Users, 1)
	require.NotNil(t, data.Detail)
	require.Equal(t, "first@example.test", data.Detail.Email)
	require.Equal(t, "openai", data.Detail.Groups[0].Platform)
	require.Equal(t, []string{"anthropic", "openai"}, data.Detail.Models[0].Platforms)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOperationsUserSuggestionsOnlyActiveOrdinaryUsers(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newUsageLogRepositoryWithSQL(nil, db)

	empty, err := repo.GetOperationsUserSuggestions(context.Background(), "")
	require.NoError(t, err)
	require.Empty(t, empty)
	mock.ExpectQuery(`SELECT id, email FROM users.*role = 'user' AND deleted_at IS NULL.*POSITION\(LOWER\(\$1\) IN LOWER\(email\)\).*LIMIT 20`).
		WithArgs("example").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email"}).AddRow(12, "first@example.test"))
	users, err := repo.GetOperationsUserSuggestions(context.Background(), "example")
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, int64(12), users[0].UserID)
	require.NoError(t, mock.ExpectationsWereMet())
}
