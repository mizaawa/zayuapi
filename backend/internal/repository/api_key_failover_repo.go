package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *apiKeyRepository) GetAPIKeyFailover(ctx context.Context, id int64) (*service.APIKey, error) {
	m, err := clientFromContext(ctx, r.client).APIKey.Query().Where(apikey.IDEQ(id), apikey.DeletedAtIsNil()).Select(
		apikey.FieldID,
		apikey.FieldUserID,
		apikey.FieldStatus,
		apikey.FieldGroupID,
		apikey.FieldFailoverEnabled,
		apikey.FieldFailoverGroupID,
		apikey.FieldFailoverMaxRetries,
		apikey.FieldFailoverCooldownSeconds,
		apikey.FieldFailoverCooldownUntil,
		apikey.FieldFailoverRevision,
	).Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrAPIKeyNotFound
		}
		return nil, err
	}
	return apiKeyEntityToService(m), nil
}

func (r *apiKeyRepository) StartAPIKeyFailoverCooldown(ctx context.Context, key *service.APIKey, now time.Time) (*time.Time, error) {
	primaryID := key.FailoverPrimaryGroupID
	if primaryID == nil {
		primaryID = key.GroupID
	}
	if primaryID == nil || key.FailoverGroupID == nil || key.FailoverCooldownSeconds <= 0 {
		return nil, service.ErrAPIKeyFailoverChanged
	}
	until := now.Add(time.Duration(key.FailoverCooldownSeconds) * time.Second)
	// The revision changes only on configuration edits/releases, not on billing updates.
	_, err := clientFromContext(ctx, r.client).APIKey.Update().Where(
		apikey.IDEQ(key.ID),
		apikey.DeletedAtIsNil(),
		apikey.StatusEQ(service.StatusActive),
		apikey.FailoverEnabledEQ(true),
		apikey.GroupIDEQ(*primaryID),
		apikey.FailoverGroupIDEQ(*key.FailoverGroupID),
		apikey.FailoverRevisionEQ(key.FailoverRevision),
		apikey.Or(apikey.FailoverCooldownUntilIsNil(), apikey.FailoverCooldownUntilLTE(now)),
	).SetFailoverCooldownUntil(until).Save(ctx)
	if err != nil {
		return nil, err
	}
	state, err := r.GetAPIKeyFailover(ctx, key.ID)
	if err != nil {
		return nil, err
	}
	if !state.IsActive() || !state.FailoverEnabled || state.FailoverRevision != key.FailoverRevision ||
		state.GroupID == nil || *state.GroupID != *primaryID || state.FailoverGroupID == nil ||
		*state.FailoverGroupID != *key.FailoverGroupID || state.FailoverCooldownUntil == nil {
		return nil, service.ErrAPIKeyFailoverChanged
	}
	return state.FailoverCooldownUntil, nil
}
