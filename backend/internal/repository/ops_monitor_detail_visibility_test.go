//go:build unit

package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpsChannelMonitorErrorDetailFiltersBeforeReading(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}
	predicate := "NOT EXISTS (SELECT 1 FROM api_keys monitor_key WHERE monitor_key.id = e.api_key_id AND monitor_key.purpose = 'channel_monitor')"
	mock.ExpectQuery("WHERE e.id = \\$1 AND " + regexp.QuoteMeta(predicate) + " LIMIT 1").
		WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, err := repo.GetErrorLogByIDWithVisibility(context.Background(), 7, true)
	require.ErrorIs(t, err, sql.ErrNoRows)
	mock.ExpectQuery("WHERE e.id = \\$1 LIMIT 1").
		WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, err = repo.GetErrorLogByID(context.Background(), 7)
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.NoError(t, mock.ExpectationsWereMet())
}
