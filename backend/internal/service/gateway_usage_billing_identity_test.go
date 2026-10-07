package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingIdentityIgnoresCallerRequestID(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "repeated-client-header")
	require.Equal(t, "upstream-first", resolveUsageBillingRequestID(ctx, "upstream-first"))
	require.Equal(t, "upstream-second", resolveUsageBillingRequestID(ctx, "upstream-second"))
	first := resolveUsageBillingRequestID(ctx, "")
	second := resolveUsageBillingRequestID(ctx, "")
	require.Contains(t, first, "generated:")
	require.NotEqual(t, first, second)
	require.Empty(t, resolveUsageBillingPayloadFingerprint(ctx, ""))
	require.Equal(t, "payload-hash", resolveUsageBillingPayloadFingerprint(ctx, "payload-hash"))
}

func TestUsageBillingIdentityKeepsServerIdentityStable(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "server-generated-id")
	ctx = context.WithValue(ctx, ctxkey.RequestID, "repeated-client-header")
	first := resolveUsageBillingRequestID(ctx, "upstream-first")
	require.Equal(t, "client:server-generated-id", first)
	require.Equal(t, first, resolveUsageBillingRequestID(ctx, "upstream-second"))
	require.Equal(t, first, resolveUsageBillingPayloadFingerprint(ctx, ""))
}
