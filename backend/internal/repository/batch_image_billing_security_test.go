package repository

import (
	"context"
	"database/sql"
	"math"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBatchImageHoldTerminalOperationsAreExclusive(t *testing.T) {
	for _, tc := range []struct {
		name, operation          string
		captured, released, held bool
		allowed, conflict        bool
	}{
		{"release-after-capture", "release", true, false, true, false, false},
		{"capture-after-release", "capture", false, true, true, false, true},
		{"reserve-after-release", "reserve", false, true, true, false, true},
		{"reserve-after-capture", "reserve", true, false, true, false, true},
		{"capture-without-reserve", "capture", false, false, false, false, true},
		{"capture-reserved", "capture", false, false, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			expectClaim := func(id string, exists bool) {
				query := mock.ExpectQuery(`SELECT 1\s+FROM usage_billing_dedup\s+WHERE request_id = \$1 AND api_key_id = \$2`).WithArgs(id, int64(9))
				if exists {
					query.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
				} else {
					query.WillReturnError(sql.ErrNoRows)
					mock.ExpectQuery(`SELECT 1\s+FROM usage_billing_dedup_archive\s+WHERE request_id = \$1 AND api_key_id = \$2`).WithArgs(id, int64(9)).WillReturnError(sql.ErrNoRows)
				}
			}
			expectClaim(service.BatchImageCaptureRequestID("batch"), tc.captured)
			if !tc.captured || tc.operation != "release" {
				expectClaim(service.BatchImageReleaseRequestID("batch"), tc.released)
				if tc.operation == "capture" && !tc.released {
					expectClaim(service.BatchImageHoldRequestID("batch"), tc.held)
				}
			}
			mock.ExpectRollback()
			allowed, err := batchImageHoldOperationAllowed(context.Background(), tx, &service.BatchImageBalanceHoldCommand{BatchID: "batch", APIKeyID: 9, HoldAmount: 1}, tc.operation)
			require.Equal(t, tc.allowed, allowed)
			if tc.conflict {
				require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestBatchImageRefundLocksBeforeCheckingCapture(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(advisoryLockHash("batch_image_billing:batch")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT 1\s+FROM usage_billing_dedup`).WithArgs(service.BatchImageCaptureRequestID("batch"), int64(9)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	mock.ExpectRollback()
	repo := &usageBillingRepository{db: db}
	result, err := repo.ReleaseBatchImageBalance(context.Background(), &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageReleaseRequestID("batch"), BatchID: "batch", UserID: 7, APIKeyID: 9, HoldAmount: 1})
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Nil(t, result.NewBalance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBatchImageHoldRejectsInvalidAmounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &usageBillingRepository{db: db}
	for _, amount := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := repo.ReserveBatchImageBalance(context.Background(), &service.BatchImageBalanceHoldCommand{RequestID: "hold", BatchID: "batch", UserID: 7, APIKeyID: 9, HoldAmount: amount})
		require.Error(t, err)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
