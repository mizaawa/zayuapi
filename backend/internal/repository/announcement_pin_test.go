package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/announcement"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestAnnouncementTogglePinReplacesAndUnpins(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:announcement_pins?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewAnnouncementRepository(client)
	updatedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := client.Announcement.Create().SetTitle("First").SetContent("First content").
		SetStatus(service.AnnouncementStatusActive).SetUpdatedAt(updatedAt).SaveX(ctx)
	second := client.Announcement.Create().SetTitle("Second").SetContent("Second content").
		SetStatus(service.AnnouncementStatusActive).SetUpdatedAt(updatedAt).SaveX(ctx)

	result, err := repo.TogglePin(ctx, first.ID)
	require.NoError(t, err)
	require.True(t, result.Announcement.IsPinned)
	require.Nil(t, result.ReplacedAnnouncementID)
	require.Equal(t, updatedAt, result.Announcement.UpdatedAt)

	active, err := repo.ListActive(ctx, time.Now())
	require.NoError(t, err)
	require.Equal(t, first.ID, active[0].ID)
	listed, _, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 20}, service.AnnouncementListFilters{})
	require.NoError(t, err)
	require.Equal(t, first.ID, listed[0].ID)

	_, err = repo.TogglePin(ctx, 9999)
	require.ErrorIs(t, err, service.ErrAnnouncementNotFound)
	require.True(t, client.Announcement.GetX(ctx, first.ID).IsPinned)

	_, err = client.Announcement.UpdateOneID(second.ID).SetIsPinned(true).Save(ctx)
	require.True(t, dbent.IsConstraintError(err), "the database must reject two pinned announcements")

	result, err = repo.TogglePin(ctx, second.ID)
	require.NoError(t, err)
	require.True(t, result.Announcement.IsPinned)
	require.Equal(t, first.ID, *result.ReplacedAnnouncementID)
	require.False(t, client.Announcement.GetX(ctx, first.ID).IsPinned)
	require.Equal(t, updatedAt, client.Announcement.GetX(ctx, first.ID).UpdatedAt)
	require.Equal(t, updatedAt, result.Announcement.UpdatedAt)
	require.Equal(t, 1, client.Announcement.Query().Where(announcement.IsPinnedEQ(true)).CountX(ctx))

	result, err = repo.TogglePin(ctx, second.ID)
	require.NoError(t, err)
	require.False(t, result.Announcement.IsPinned)
	require.Nil(t, result.ReplacedAnnouncementID)
	require.Equal(t, updatedAt, result.Announcement.UpdatedAt)
	require.Zero(t, client.Announcement.Query().Where(announcement.IsPinnedEQ(true)).CountX(ctx))
}

func TestAnnouncementTogglePinLocksBeforeReading(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_xact_lock($1)")).
		WithArgs(advisoryLockHash("announcements:pin")).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_xact_lock"}).AddRow(nil))
	mock.ExpectQuery(`SELECT .* FROM "announcements" WHERE .*`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows(announcement.Columns))
	mock.ExpectRollback()

	_, err = NewAnnouncementRepository(client).TogglePin(context.Background(), 99)
	require.ErrorIs(t, err, service.ErrAnnouncementNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
