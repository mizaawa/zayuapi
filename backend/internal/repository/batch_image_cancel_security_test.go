package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBatchImageCancelRechecksSubmissionStatusUnderLock(t *testing.T) {
	for _, status := range []string{service.BatchImageJobStatusUploading, service.BatchImageJobStatusSubmitted, service.BatchImageJobStatusRunning} {
		t.Run(status, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT status FROM batch_image_jobs WHERE batch_id = \$1 FOR UPDATE`).WithArgs("batch").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(status))
			mock.ExpectRollback()
			repo := &batchImageRepository{db: db, sql: db}
			err = repo.TransitionBatchImageJobStatus(context.Background(), "batch", service.BatchImageJobStatusCancelled, service.BatchImageTransitionOptions{ExpectedStatus: service.BatchImageJobStatusCreated})
			require.ErrorIs(t, err, service.ErrBatchImageInvalidTransition)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
