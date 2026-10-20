package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBatchImageHoldRejectsForeignRequestIDs(t *testing.T) {
	for _, operation := range []string{"reserve", "capture", "release"} {
		for _, requestID := range []string{"chat:request", "token_hold:request", service.BatchImageHoldRequestID("other-batch")} {
			t.Run(operation+"/"+requestID, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				repo := &usageBillingRepository{db: db}
				cmd := &service.BatchImageBalanceHoldCommand{RequestID: requestID, BatchID: "batch", UserID: 7, APIKeyID: 9, HoldAmount: 1}
				switch operation {
				case "reserve":
					_, err = repo.ReserveBatchImageBalance(context.Background(), cmd)
				case "capture":
					_, err = repo.CaptureBatchImageBalance(context.Background(), cmd)
				case "release":
					_, err = repo.ReleaseBatchImageBalance(context.Background(), cmd)
				}
				require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
				require.NoError(t, mock.ExpectationsWereMet(), "invalid identities must never reach a money transaction")
			})
		}
	}
}

func TestBatchImageHoldRequiresMatchingPersistedJob(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(advisoryLockHash("batch_image_billing:batch")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT EXISTS.*FROM batch_image_jobs`).WithArgs("batch", int64(7), int64(9), 1.0).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()
	_, err = (&usageBillingRepository{db: db}).ReserveBatchImageBalance(context.Background(), &service.BatchImageBalanceHoldCommand{
		RequestID: service.BatchImageHoldRequestID("batch"), BatchID: "batch", UserID: 7, APIKeyID: 9, HoldAmount: 1,
	})
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}
