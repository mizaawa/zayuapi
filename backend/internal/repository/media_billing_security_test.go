package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMediaSettlementTerminalStateProtection(t *testing.T) {
	for _, tc := range []struct {
		name, state                                 string
		refund, refundPending, finalQuote, conflict bool
		delivery                                    bool
	}{
		{name: "repeat-settlement", state: "settled"},
		{name: "settlement-after-refund", state: "refunded", conflict: true},
		{name: "settlement-after-staged-refund", state: "pending", refundPending: true, conflict: true},
		{name: "repeat-refund", state: "refunded", refund: true},
		{name: "refund-after-settlement", state: "settled", refund: true},
		{name: "refund-after-staged-success", state: "pending", refund: true, finalQuote: true, conflict: true},
		{name: "workbench-before-delivery", state: "pending", delivery: true, conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &usageBillingRepository{db: db}
			reservation := &service.MediaBillingReservation{ID: "media:state", MediaProtocol: "8yes", Quote: service.MediaBillingQuote{Command: service.UsageBillingCommand{RequestID: "media:state", UserID: 7, APIKeyID: 9, BalanceCost: 1}}}
			reservation.WorkbenchDelivery = tc.delivery
			encoded, err := json.Marshal(reservation)
			require.NoError(t, err)
			var final []byte
			if tc.finalQuote {
				final, err = json.Marshal(reservation.Quote)
				require.NoError(t, err)
			}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT quote,state,task_id,endpoint,final_quote,refund_requested,queue_lease_until,dispatch_started_at FROM media_billing_reservations WHERE id=\$1 FOR UPDATE`).
				WithArgs(reservation.ID).
				WillReturnRows(sqlmock.NewRows([]string{"quote", "state", "task_id", "endpoint", "final_quote", "refund_requested", "queue_lease_until", "dispatch_started_at"}).AddRow(encoded, tc.state, "task", "videos", final, tc.refundPending, nil, nil))
			mock.ExpectRollback()
			if tc.refund {
				var refunded *service.MediaBillingReservation
				refunded, err = repo.RefundMedia(context.Background(), reservation.ID)
				require.Nil(t, refunded)
			} else {
				var settled *service.UsageBillingApplyResult
				settled, err = repo.SettleMedia(context.Background(), reservation.ID, &reservation.Quote.Command)
				if settled != nil {
					require.False(t, settled.Applied)
				}
			}
			if tc.conflict {
				require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet(), "terminal state must not produce balance or quota writes")
		})
	}
}
