package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func newRedeemCodeSecurityRepoMock(t *testing.T) (*redeemCodeRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return &redeemCodeRepository{client: client}, mock
}

func TestRedeemBatchUpdateLocksStatusBeforeEditing(t *testing.T) {
	repo, mock := newRedeemCodeSecurityRepoMock(t)
	status := service.StatusUnused
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "redeem_codes" WHERE "redeem_codes"\."id" IN \(\$1\) FOR UPDATE`).
		WithArgs(int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{redeemcode.FieldID, redeemcode.FieldStatus}).AddRow(int64(12), service.StatusUsed))
	mock.ExpectRollback()
	_, err := repo.BatchUpdate(context.Background(), []int64{12}, service.RedeemCodeBatchUpdateFields{Status: &status})
	require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRedeemUseChecksExpiryInAtomicUpdate(t *testing.T) {
	repo, mock := newRedeemCodeSecurityRepoMock(t)
	mock.ExpectExec(`UPDATE "redeem_codes" SET .* WHERE .*"status" = .*\("redeem_codes"\."expires_at" IS NULL OR "redeem_codes"\."expires_at" >`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	err := repo.Use(context.Background(), 12, 34)
	require.ErrorIs(t, err, service.ErrRedeemCodeUsed)
	require.NoError(t, mock.ExpectationsWereMet())
}
