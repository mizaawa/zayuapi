package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

var paymentAccessUpdates = struct {
	sync.Mutex
	signal chan struct{}
}{signal: make(chan struct{})}

// Wake open browser checks after committed permission changes. Periodic reads
// also observe writes handled by another server process.
func PaymentAccessChanged() {
	paymentAccessUpdates.Lock()
	close(paymentAccessUpdates.signal)
	paymentAccessUpdates.signal = make(chan struct{})
	paymentAccessUpdates.Unlock()
}

func (a *PaymentAccess) Fingerprint() string {
	return fmt.Sprintf("%t:%t:%t", a.Enabled, a.Allowed, a.ExclusiveEnabled)
}

func (s *PaymentService) WaitForPaymentAccess(ctx context.Context, userID int64, previous string, timeout time.Duration) (*PaymentAccess, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		paymentAccessUpdates.Lock()
		signal := paymentAccessUpdates.signal
		paymentAccessUpdates.Unlock()
		access, err := s.GetPaymentAccess(ctx, userID)
		if err != nil || access.Fingerprint() != previous {
			return access, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return s.GetPaymentAccess(ctx, userID)
		case <-signal:
		case <-ticker.C:
		}
	}
}
