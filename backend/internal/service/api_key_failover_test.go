package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type failoverKeyRepoStub struct {
	APIKeyRepository
	key    *APIKey
	fields APIKeyUpdateFields
}

func (r *failoverKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	clone := *r.key
	return &clone, nil
}

func (r *failoverKeyRepoStub) GetAPIKeyFailover(ctx context.Context, id int64) (*APIKey, error) {
	return r.GetByID(ctx, id)
}

func (r *failoverKeyRepoStub) Update(_ context.Context, key *APIKey, fields APIKeyUpdateFields) error {
	r.fields = fields
	if fields.FailoverConfig || fields.FailoverCooldown {
		key.FailoverRevision++
	}
	clone := *key
	r.key = &clone
	return nil
}

func (r *failoverKeyRepoStub) StartAPIKeyFailoverCooldown(_ context.Context, key *APIKey, now time.Time) (*time.Time, error) {
	if !r.key.FailoverEnabled || key.FailoverRevision != r.key.FailoverRevision {
		return nil, ErrAPIKeyFailoverChanged
	}
	if r.key.FailoverCooldownUntil == nil || !now.Before(*r.key.FailoverCooldownUntil) {
		until := now.Add(time.Duration(key.FailoverCooldownSeconds) * time.Second)
		r.key.FailoverCooldownUntil = &until
	}
	return r.key.FailoverCooldownUntil, nil
}

type failoverGroupRepoStub struct {
	GroupRepository
	groups map[int64]*Group
}

func (r *failoverGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	if g := r.groups[id]; g != nil {
		clone := *g
		return &clone, nil
	}
	return nil, ErrGroupNotFound
}

type failoverUserRepoStub struct {
	UserRepository
	user *User
}

func (r *failoverUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	clone := *r.user
	return &clone, nil
}

type failoverSubscriptionRepoStub struct {
	UserSubscriptionRepository
}

func (r *failoverSubscriptionRepoStub) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	return &UserSubscription{UserID: userID, GroupID: groupID, Status: StatusActive}, nil
}

func newFailoverTestService() (*APIKeyService, *failoverKeyRepoStub, *failoverGroupRepoStub, *failoverUserRepoStub) {
	primaryID, fallbackID := int64(10), int64(20)
	groups := &failoverGroupRepoStub{groups: map[int64]*Group{
		primaryID:  {ID: primaryID, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1},
		fallbackID: {ID: fallbackID, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 2},
	}}
	repo := &failoverKeyRepoStub{key: &APIKey{
		ID: 1, UserID: 7, Key: "sk-failover-test", Status: StatusActive, GroupID: &primaryID,
		Group: groups.groups[primaryID], FailoverEnabled: true, FailoverGroupID: &fallbackID,
		FailoverMaxRetries: 3, FailoverCooldownSeconds: 300,
	}}
	users := &failoverUserRepoStub{user: &User{ID: 7, Status: StatusActive}}
	svc := &APIKeyService{apiKeyRepo: repo, groupRepo: groups, userRepo: users, userSubRepo: &failoverSubscriptionRepoStub{}}
	return svc, repo, groups, users
}

func TestAPIKeyFailoverConfigurationValidation(t *testing.T) {
	for _, n := range []int{0, 11} {
		svc, _, _, _ := newFailoverTestService()
		_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{FailoverMaxRetries: &n})
		require.ErrorIs(t, err, ErrAPIKeyFailoverInvalid)
	}
	zero := 0
	svc, _, _, _ := newFailoverTestService()
	_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{FailoverCooldownSeconds: &zero})
	require.ErrorIs(t, err, ErrAPIKeyFailoverInvalid)

	for _, platform := range []string{PlatformAnthropic, PlatformCustom, PlatformGemini} {
		svc, _, groups, _ := newFailoverTestService()
		groups.groups[30] = &Group{ID: 30, Status: StatusActive, Platform: platform}
		id := int64(30)
		_, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{FailoverGroupID: &id})
		require.ErrorIs(t, err, ErrAPIKeyFailoverInvalid)
	}
	svc, _, _, _ = newFailoverTestService()
	primaryID := int64(10)
	_, err = svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{FailoverGroupID: &primaryID})
	require.ErrorIs(t, err, ErrAPIKeyFailoverInvalid)
}

func TestAPIKeyFailoverSupportsSamePlatformPairsExceptCustom(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformCustom, PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			svc, repo, groups, _ := newFailoverTestService()
			groups.groups[10].Platform = platform
			groups.groups[20].Platform = platform
			attempts := 1
			key, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{FailoverMaxRetries: &attempts})
			if platform == PlatformCustom || platform == PlatformComposite {
				require.ErrorIs(t, err, ErrAPIKeyFailoverInvalid)
			} else {
				require.NoError(t, err)
				fallback, err := svc.PrepareAPIKeyFailover(context.Background(), key)
				require.NoError(t, err)
				require.Equal(t, platform, fallback.Group.Platform)
				require.NoError(t, svc.StartAPIKeyFailoverCooldown(context.Background(), fallback))
				resolved, err := svc.ResolveAPIKeyFailover(context.Background(), key)
				require.NoError(t, err)
				require.True(t, resolved.FailoverActive)
				require.Equal(t, int64(20), *resolved.GroupID)
				require.Equal(t, int64(10), *repo.key.GroupID)
			}
		})
	}
}

func TestAPIKeyFailoverAllowsCrossBillingTypeAndEnforcesPermission(t *testing.T) {
	svc, _, groups, users := newFailoverTestService()
	groups.groups[30] = &Group{ID: 30, Status: StatusActive, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription}
	id := int64(30)
	key, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{FailoverGroupID: &id})
	require.NoError(t, err)
	require.Equal(t, id, *key.FailoverGroupID)
	groups.groups[30].SubscriptionType = SubscriptionTypeStandard
	groups.groups[30].IsExclusive = true
	_, err = svc.PrepareAPIKeyFailover(context.Background(), key)
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	users.user.AllowedGroups = []int64{30}
	_, err = svc.PrepareAPIKeyFailover(context.Background(), key)
	require.NoError(t, err)
	groups.groups[30].Status = StatusDisabled
	_, err = svc.PrepareAPIKeyFailover(context.Background(), key)
	require.ErrorIs(t, err, ErrAPIKeyFailoverInvalid)
}

func TestAPIKeyFailoverResolveUsesDatabaseCooldownAndKeepsPrimary(t *testing.T) {
	svc, repo, _, _ := newFailoverTestService()
	cached := *repo.key
	until := time.Now().Add(5 * time.Minute)
	repo.key.FailoverCooldownUntil = &until
	resolved, err := svc.ResolveAPIKeyFailover(context.Background(), &cached)
	require.NoError(t, err)
	require.True(t, resolved.FailoverActive)
	require.Equal(t, int64(20), *resolved.GroupID)
	require.Equal(t, float64(2), resolved.Group.RateMultiplier)
	require.Equal(t, int64(10), *resolved.FailoverPrimaryGroupID)
	require.Equal(t, int64(10), *cached.GroupID)
	require.Equal(t, int64(10), *repo.key.GroupID)
	cached.FailoverCooldownUntil = &until
	repo.key.FailoverCooldownUntil = nil
	resolved, err = svc.ResolveAPIKeyFailover(context.Background(), &cached)
	require.NoError(t, err)
	require.False(t, resolved.FailoverActive)
	require.Equal(t, int64(10), *resolved.GroupID)
	expired := time.Now().Add(-time.Second)
	repo.key.FailoverCooldownUntil = &expired
	resolved, err = svc.ResolveAPIKeyFailover(context.Background(), &cached)
	require.NoError(t, err)
	require.False(t, resolved.FailoverActive)
	require.Equal(t, int64(10), *resolved.GroupID)
}

func TestAPIKeyFailoverReleaseProtectsAgainstInflightActivation(t *testing.T) {
	svc, repo, _, _ := newFailoverTestService()
	prepared, err := svc.PrepareAPIKeyFailover(context.Background(), repo.key)
	require.NoError(t, err)
	released, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{ReleaseFailoverCooldown: true})
	require.NoError(t, err)
	require.True(t, released.FailoverEnabled)
	require.Equal(t, int64(20), *released.FailoverGroupID)
	require.Equal(t, APIKeyUpdateFields{FailoverCooldown: true}, repo.fields)
	require.ErrorIs(t, svc.StartAPIKeyFailoverCooldown(context.Background(), prepared), ErrAPIKeyFailoverChanged)
	require.Nil(t, repo.key.FailoverCooldownUntil)
}

type failoverRPMOverrideRepoStub struct {
	UserGroupRateRepository
	groupID  int64
	override *int
}

func (r *failoverRPMOverrideRepoStub) GetRPMOverrideByUserAndGroup(_ context.Context, _, groupID int64) (*int, error) {
	r.groupID = groupID
	return r.override, nil
}

func TestAPIKeyFailoverPrimaryChangeRefreshesRPMOverride(t *testing.T) {
	for _, reload := range []bool{false, true} {
		t.Run(map[bool]string{false: "clear stale override", true: "load new group override"}[reload], func(t *testing.T) {
			svc, repo, groups, users := newFailoverTestService()
			oldOverride, newOverride := 0, 2
			cached := *repo.key
			cached.User = users.user
			cached.User.UserGroupRPMOverride = &oldOverride
			newPrimaryID := int64(30)
			groups.groups[newPrimaryID] = &Group{ID: newPrimaryID, Platform: PlatformOpenAI, Status: StatusActive, RPMLimit: 1}
			repo.key.GroupID = &newPrimaryID
			var rates *failoverRPMOverrideRepoStub
			if reload {
				rates = &failoverRPMOverrideRepoStub{override: &newOverride}
				svc.userGroupRateRepo = rates
			}
			resolved, err := svc.ResolveAPIKeyFailover(context.Background(), &cached)
			require.NoError(t, err)
			require.Equal(t, newPrimaryID, *resolved.GroupID)
			require.NotSame(t, cached.User, resolved.User)
			require.Equal(t, 0, *cached.User.UserGroupRPMOverride, "refresh must not mutate the cached user's override")
			if reload {
				require.Equal(t, newPrimaryID, rates.groupID)
				require.Equal(t, newOverride, *resolved.User.UserGroupRPMOverride)
			} else {
				require.Nil(t, resolved.User.UserGroupRPMOverride, "the old group's unlimited override must not bypass the new group's RPM limit")
			}
		})
	}
}

func TestAPIKeyFailoverResolvePreservesLocalRejectionStatus(t *testing.T) {
	for _, status := range []string{StatusAPIKeyExpired, StatusAPIKeyQuotaExhausted, StatusAPIKeyDisabled} {
		t.Run(status, func(t *testing.T) {
			svc, repo, groups, _ := newFailoverTestService()
			cached := *repo.key
			until := time.Now().Add(time.Minute)
			repo.key.FailoverCooldownUntil = &until
			repo.key.Status = status
			delete(groups.groups, 20)

			resolved, err := svc.ResolveAPIKeyFailover(context.Background(), &cached)
			require.NoError(t, err)
			require.Equal(t, status, resolved.Status)
			require.Equal(t, int64(10), *resolved.GroupID)
			require.False(t, resolved.FailoverActive)
			_, err = svc.PrepareAPIKeyFailover(context.Background(), &cached)
			require.ErrorIs(t, err, ErrAPIKeyFailoverChanged)
		})
	}
}

func TestAPIKeyFailoverConfigurationEditClearsCoolingWithoutUsageWrites(t *testing.T) {
	svc, repo, _, _ := newFailoverTestService()
	until := time.Now().Add(time.Minute)
	repo.key.FailoverCooldownUntil = &until
	repo.key.QuotaUsed, repo.key.Usage5h = 42, 15
	attempts := 10
	updated, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{FailoverMaxRetries: &attempts})
	require.NoError(t, err)
	require.Nil(t, updated.FailoverCooldownUntil)
	require.Equal(t, APIKeyUpdateFields{FailoverConfig: true, FailoverCooldown: true}, repo.fields)
	require.Equal(t, float64(42), updated.QuotaUsed)
	require.Equal(t, float64(15), updated.Usage5h)
}

func TestAPIKeyFailoverPlatformChangeDisablesOldFallback(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformCustom, PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			svc, repo, groups, _ := newFailoverTestService()
			until := time.Now().Add(time.Minute)
			repo.key.FailoverCooldownUntil = &until
			groups.groups[30] = &Group{ID: 30, Status: StatusActive, Platform: platform}
			id := int64(30)
			updated, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{GroupID: &id})
			require.NoError(t, err)
			require.False(t, updated.FailoverEnabled)
			require.Nil(t, updated.FailoverGroupID)
			require.Nil(t, updated.FailoverCooldownUntil)
			require.Equal(t, id, *updated.GroupID)
			require.Equal(t, APIKeyUpdateFields{GroupID: true, FailoverConfig: true, FailoverCooldown: true}, repo.fields)
		})
	}
}

func TestAPIKeyFailoverAuthSnapshotRoundTrip(t *testing.T) {
	svc, repo, _, users := newFailoverTestService()
	repo.key.User = users.user
	until := time.Now().Add(time.Minute)
	repo.key.FailoverCooldownUntil = &until
	repo.key.FailoverRevision = 12
	snapshot := svc.snapshotFromAPIKey(context.Background(), repo.key)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)
	key := svc.snapshotToAPIKey(repo.key.Key, snapshot)
	require.True(t, key.FailoverEnabled)
	require.Equal(t, repo.key.FailoverGroupID, key.FailoverGroupID)
	require.Equal(t, 3, key.FailoverMaxRetries)
	require.Equal(t, 300, key.FailoverCooldownSeconds)
	require.Equal(t, &until, key.FailoverCooldownUntil)
	require.Equal(t, int64(12), key.FailoverRevision)
}
