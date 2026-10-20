package service

import "context"

// A delivery receipt outlives a deleted task until its payment is finalized.
// Stores write it atomically with the successful task transition.
type MediaWorkbenchDeliveryReceipt struct {
	TaskID    string `json:"task_id"`
	UserID    int64  `json:"user_id"`
	APIKeyID  int64  `json:"api_key_id"`
	BillingID string `json:"billing_id"`
}

type MediaWorkbenchDeliveryStore interface {
	GetMediaDelivery(context.Context, string) (*MediaWorkbenchDeliveryReceipt, error)
	DeleteMediaDelivery(context.Context, string, string) error
}

func mediaDeliveryMatches(receipt *MediaWorkbenchDeliveryReceipt, r *MediaBillingReservation, taskID string) bool {
	return receipt != nil && receipt.TaskID == taskID && receipt.BillingID == r.ID &&
		receipt.UserID == r.Quote.Command.UserID && receipt.APIKeyID == r.Quote.Command.APIKeyID
}
