//go:build unit

package repository

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestChannelMonitorBlankNamePersistsAndUsesLiveGroupName(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	group, err := client.Group.Create().SetName("Original group").SetPlatform("openai").Save(ctx)
	require.NoError(t, err)
	repo := NewChannelMonitorRepository(client, db)
	monitor := &service.ChannelMonitor{
		Provider:        "openai",
		GroupID:         &group.ID,
		GroupName:       group.Name,
		Endpoint:        "https://example.com",
		APIKey:          "encrypted-test-key",
		PrimaryModel:    "gpt-5",
		IntervalSeconds: 60,
		CreatedBy:       1,
	}
	require.NoError(t, repo.Create(ctx, monitor))
	stored, err := repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.Empty(t, stored.Name)
	require.Equal(t, "Original group", stored.GroupName)

	_, err = client.Group.UpdateOneID(group.ID).SetName("Renamed group").Save(ctx)
	require.NoError(t, err)
	stored, err = repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.Empty(t, stored.Name)
	require.Equal(t, "Renamed group", stored.GroupName)
	items, total, err := repo.List(ctx, service.ChannelMonitorListParams{Search: "Renamed", Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	require.Empty(t, items[0].Name)
	require.Equal(t, "Renamed group", items[0].GroupName)

	stored.Name = "Custom monitor"
	require.NoError(t, repo.Update(ctx, stored))
	stored.Name = ""
	require.NoError(t, repo.Update(ctx, stored))
	stored, err = repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.Empty(t, stored.Name)
	require.Equal(t, "Renamed group", stored.GroupName)
}
