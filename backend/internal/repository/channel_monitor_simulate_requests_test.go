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

func TestChannelMonitorRepositoryPersistsSimulateRequests(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewChannelMonitorRepository(client, db)
	ctx := context.Background()
	monitor := &service.ChannelMonitor{
		Name:                 "Simulated monitor",
		Provider:             "openai",
		Endpoint:             "https://example.com",
		APIKey:               "encrypted-test-key",
		PrimaryModel:         "gpt-5",
		Enabled:              true,
		SimulateRequests:     true,
		IntervalSeconds:      60,
		CreatedBy:            1,
		DuplicateOperationID: "simulated-duplicate",
	}

	require.NoError(t, repo.Create(ctx, monitor))
	stored, err := repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.True(t, stored.SimulateRequests)

	items, total, err := repo.List(ctx, service.ChannelMonitorListParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.True(t, items[0].SimulateRequests)

	enabled, err := repo.ListEnabled(ctx)
	require.NoError(t, err)
	require.Len(t, enabled, 1)
	require.True(t, enabled[0].SimulateRequests)

	duplicate, err := repo.FindByDuplicateOperationID(ctx, monitor.DuplicateOperationID)
	require.NoError(t, err)
	require.NotNil(t, duplicate)
	require.True(t, duplicate.SimulateRequests)

	stored.SimulateRequests = false
	require.NoError(t, repo.Update(ctx, stored))
	stored, err = repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.False(t, stored.SimulateRequests)

	stored.SimulateRequests = true
	require.NoError(t, repo.Update(ctx, stored))
	stored, err = repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.True(t, stored.SimulateRequests)

	legacy, err := client.ChannelMonitor.Create().
		SetName("Legacy monitor").
		SetProvider("openai").
		SetEndpoint("https://example.com").
		SetAPIKeyEncrypted("encrypted-test-key").
		SetPrimaryModel("gpt-5").
		SetIntervalSeconds(60).
		SetCreatedBy(1).
		Save(ctx)
	require.NoError(t, err)
	stored, err = repo.GetByID(ctx, legacy.ID)
	require.NoError(t, err)
	require.False(t, stored.SimulateRequests)
}
