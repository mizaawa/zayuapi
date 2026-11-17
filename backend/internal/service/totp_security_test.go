package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

type totpSecurityCache struct {
	TotpCache
	attempts atomic.Int64
	err      error
}

func (c *totpSecurityCache) GetVerifyAttempts(context.Context, int64) (int, error) {
	return 0, c.err // Simulates concurrent readers seeing the old count.
}
func (c *totpSecurityCache) IncrementVerifyAttempts(context.Context, int64) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	return int(c.attempts.Add(1)), nil
}
func (c *totpSecurityCache) ClearVerifyAttempts(context.Context, int64) error {
	c.attempts.Store(0)
	return nil
}

type totpSecurityUserRepo struct {
	UserRepository
	reads atomic.Int64
	user  User
}

func (r *totpSecurityUserRepo) GetByID(context.Context, int64) (*User, error) {
	r.reads.Add(1)
	user := r.user
	return &user, nil
}

type totpSecurityEncryptor struct{}

func (totpSecurityEncryptor) Encrypt(value string) (string, error) { return value, nil }
func (totpSecurityEncryptor) Decrypt(value string) (string, error) { return value, nil }

func TestTOTPVerificationAttemptLimitIsConcurrentAndFailClosed(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	repo := &totpSecurityUserRepo{user: User{ID: 1, TotpEnabled: true, TotpSecretEncrypted: &secret}}
	cache := &totpSecurityCache{}
	svc := NewTotpService(repo, totpSecurityEncryptor{}, cache, nil, nil, nil)
	const requests = 32
	start := make(chan struct{})
	errorsOut := make(chan error, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errorsOut <- svc.VerifyCode(context.Background(), 1, "bad-code")
		}()
	}
	close(start)
	wg.Wait()
	close(errorsOut)
	invalid, limited := 0, 0
	for err := range errorsOut {
		switch {
		case errors.Is(err, ErrTotpInvalidCode):
			invalid++
		case errors.Is(err, ErrTotpTooManyAttempts):
			limited++
		default:
			t.Fatalf("unexpected verification error: %v", err)
		}
	}
	require.Equal(t, maxTotpAttempts, invalid)
	require.Equal(t, requests-maxTotpAttempts, limited)
	require.Equal(t, int64(maxTotpAttempts), repo.reads.Load())

	for _, failedCache := range []TotpCache{nil, &totpSecurityCache{err: errors.New("redis unavailable")}} {
		closedRepo := &totpSecurityUserRepo{}
		closedService := NewTotpService(closedRepo, nil, failedCache, nil, nil, nil)
		require.ErrorIs(t, closedService.VerifyCode(context.Background(), 1, "123456"), ErrTotpUnavailable)
		require.Zero(t, closedRepo.reads.Load())
	}
}

func TestTOTPVerificationDoesNotLogSecretFragments(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	secret := "JBSWY3DPEHPK3PXP"
	repo := &totpSecurityUserRepo{user: User{ID: 1, TotpEnabled: true, TotpSecretEncrypted: &secret}}
	cache := &totpSecurityCache{}
	svc := NewTotpService(repo, totpSecurityEncryptor{}, cache, nil, nil, nil)
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	require.NoError(t, svc.VerifyCode(context.Background(), 1, code))
	require.Zero(t, cache.attempts.Load(), "successful verification clears failed-attempt history")
	require.NotContains(t, output.String(), secret[:4])
	require.NotContains(t, output.String(), "secret_prefix")
	require.NotContains(t, output.String(), "decrypted_prefix")
}
