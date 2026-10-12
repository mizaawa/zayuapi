//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type groupMonitorRepoStub struct {
	ChannelMonitorRepository
	existing  *ChannelMonitor
	created   []*ChannelMonitor
	updated   []*ChannelMonitor
	deleted   []int64
	createErr error
	updateErr error
	deleteErr error
	nextID    int64
}

func (r *groupMonitorRepoStub) Create(_ context.Context, monitor *ChannelMonitor) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.nextID++
	monitor.ID = 100 + r.nextID
	r.created = append(r.created, cloneGroupMonitor(monitor))
	return nil
}

func (r *groupMonitorRepoStub) GetByID(_ context.Context, id int64) (*ChannelMonitor, error) {
	if r.existing == nil || r.existing.ID != id {
		return nil, ErrChannelMonitorNotFound
	}
	return cloneGroupMonitor(r.existing), nil
}

func (r *groupMonitorRepoStub) Update(_ context.Context, monitor *ChannelMonitor) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updated = append(r.updated, cloneGroupMonitor(monitor))
	return nil
}

func (r *groupMonitorRepoStub) Delete(_ context.Context, id int64) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.deleted = append(r.deleted, id)
	return nil
}

func (r *groupMonitorRepoStub) FindByDuplicateOperationID(context.Context, string) (*ChannelMonitor, error) {
	return nil, nil
}

func cloneGroupMonitor(source *ChannelMonitor) *ChannelMonitor {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.GroupID = cloneInt64Pointer(source.GroupID)
	cloned.TemplateID = cloneInt64Pointer(source.TemplateID)
	cloned.ExtraModels = append([]string(nil), source.ExtraModels...)
	cloned.ExtraHeaders = cloneChannelMonitorHeaders(source.ExtraHeaders)
	return &cloned
}

type groupMonitorReaderStub struct {
	groups map[int64]*Group
}

func (r *groupMonitorReaderStub) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	group := r.groups[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	cloned := *group
	return &cloned, nil
}

type groupMonitorKeyCreateCall struct {
	userID      int64
	groupID     int64
	monitorName string
}

type groupMonitorKeyDeleteCall struct {
	rawKey string
	userID int64
}

type groupMonitorKeyManagerStub struct {
	nextKeys    []string
	createCalls []groupMonitorKeyCreateCall
	deleteCalls []groupMonitorKeyDeleteCall
	createErr   error
	deleteErr   error
}

func (m *groupMonitorKeyManagerStub) CreateChannelMonitorKey(_ context.Context, userID, groupID int64, monitorName string) (*APIKey, error) {
	m.createCalls = append(m.createCalls, groupMonitorKeyCreateCall{
		userID:      userID,
		groupID:     groupID,
		monitorName: monitorName,
	})
	if m.createErr != nil {
		return nil, m.createErr
	}
	key := fmt.Sprintf("managed-key-%d", len(m.createCalls))
	if len(m.nextKeys) > 0 {
		key = m.nextKeys[0]
		m.nextKeys = m.nextKeys[1:]
	}
	return &APIKey{Key: key}, nil
}

func (m *groupMonitorKeyManagerStub) DeleteChannelMonitorKey(_ context.Context, rawKey string, userID int64) error {
	m.deleteCalls = append(m.deleteCalls, groupMonitorKeyDeleteCall{rawKey: rawKey, userID: userID})
	return m.deleteErr
}

type groupMonitorEncryptor struct {
	encryptErr    error
	decryptErr    error
	encryptInputs []string
	decryptInputs []string
}

func (e *groupMonitorEncryptor) Encrypt(plaintext string) (string, error) {
	e.encryptInputs = append(e.encryptInputs, plaintext)
	if e.encryptErr != nil {
		return "", e.encryptErr
	}
	return "enc:" + plaintext, nil
}

func (e *groupMonitorEncryptor) Decrypt(ciphertext string) (string, error) {
	e.decryptInputs = append(e.decryptInputs, ciphertext)
	if e.decryptErr != nil {
		return "", e.decryptErr
	}
	if !strings.HasPrefix(ciphertext, "enc:") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(ciphertext, "enc:"), nil
}

func newGroupMonitorService(
	repo *groupMonitorRepoStub,
	reader *groupMonitorReaderStub,
	keys *groupMonitorKeyManagerStub,
	encryptor *groupMonitorEncryptor,
) *ChannelMonitorService {
	svc := NewChannelMonitorService(repo, encryptor)
	svc.SetGroupDependencies(reader, keys)
	svc.SetManagedGatewayEndpoint("http://127.0.0.1:8080")
	attestor, _ := NewChannelMonitorAttestor(strings.Repeat("42", 32))
	svc.SetManagedGatewayAttestor(attestor)
	return svc
}

func groupMonitorCreateParams(provider string, groupID int64) ChannelMonitorCreateParams {
	return ChannelMonitorCreateParams{
		Provider:        provider,
		GroupID:         groupID,
		Endpoint:        "https://8.8.8.8",
		PrimaryModel:    "test-model",
		Enabled:         true,
		IntervalSeconds: 60,
		CreatedBy:       9,
	}
}

func activeMonitorGroup(id int64, name, platform string) *Group {
	return &Group{
		ID:             id,
		Name:           name,
		Platform:       platform,
		RateMultiplier: 0.1,
		Status:         StatusActive,
	}
}

func TestChannelMonitorCreateKeepsCustomNameAndCreatesDedicatedKey(t *testing.T) {
	repo := &groupMonitorRepoStub{}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "OpenAI Standard", MonitorProviderOpenAI),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"dedicated-key"}}
	encryptor := &groupMonitorEncryptor{}
	svc := newGroupMonitorService(repo, reader, keys, encryptor)
	params := groupMonitorCreateParams(MonitorProviderOpenAI, 1)
	params.Name = "  Primary monitor  "

	monitor, err := svc.Create(context.Background(), params)

	require.NoError(t, err)
	require.Equal(t, "Primary monitor", monitor.Name)
	require.Empty(t, monitor.APIKey, "management response must not carry the generated key")
	require.Equal(t, int64(1), *monitor.GroupID)
	require.Equal(t, 0.1, monitor.GroupRateMultiplier)
	require.Equal(t, MonitorProviderOpenAI, monitor.GroupPlatform)
	require.Equal(t, []groupMonitorKeyCreateCall{{userID: 9, groupID: 1, monitorName: "Primary monitor"}}, keys.createCalls)
	require.Equal(t, []string{"dedicated-key"}, encryptor.encryptInputs)
	require.Len(t, repo.created, 1)
	require.Equal(t, "enc:dedicated-key", repo.created[0].APIKey)
}

func TestChannelMonitorCreateGroupFailsClosedWithoutManagedGateway(t *testing.T) {
	repo := &groupMonitorRepoStub{}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "Custom Group", PlatformComposite),
	}}
	keys := &groupMonitorKeyManagerStub{}
	svc := NewChannelMonitorService(repo, &groupMonitorEncryptor{})
	svc.SetGroupDependencies(reader, keys)

	monitor, err := svc.Create(context.Background(), groupMonitorCreateParams(MonitorProviderCustom, 1))

	require.Nil(t, monitor)
	require.ErrorIs(t, err, ErrChannelMonitorManagedGatewayUnavailable)
	require.Empty(t, keys.createCalls)
	require.Empty(t, repo.created)
}

func TestChannelMonitorCreatePinsManagedGatewayWithoutClientEndpoint(t *testing.T) {
	repo := &groupMonitorRepoStub{}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "Composite", PlatformComposite),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"dedicated-key"}}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})
	svc.SetManagedGatewayEndpoint("http://127.0.0.1:8080")
	params := groupMonitorCreateParams(MonitorProviderCustom, 1)
	params.Endpoint = ""

	monitor, err := svc.Create(context.Background(), params)

	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:8080", monitor.Endpoint)
	require.Equal(t, PlatformComposite, monitor.GroupPlatform)
	require.Len(t, keys.createCalls, 1)
}

func TestChannelMonitorCreateRejectsCrossPlatformGroup(t *testing.T) {
	repo := &groupMonitorRepoStub{}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "Anthropic", MonitorProviderAnthropic),
	}}
	keys := &groupMonitorKeyManagerStub{}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})

	monitor, err := svc.Create(context.Background(), groupMonitorCreateParams(MonitorProviderOpenAI, 1))

	require.Nil(t, monitor)
	require.ErrorIs(t, err, ErrChannelMonitorGroupPlatformMismatch)
	require.Empty(t, keys.createCalls)
	require.Empty(t, repo.created)
}

func TestChannelMonitorCreateCustomAcceptsCompositeGroup(t *testing.T) {
	repo := &groupMonitorRepoStub{}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "Composite", PlatformComposite),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"custom-key"}}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})

	monitor, err := svc.Create(context.Background(), groupMonitorCreateParams(MonitorProviderCustom, 1))

	require.NoError(t, err)
	require.Equal(t, int64(1), *monitor.GroupID)
	require.Empty(t, monitor.Name)
	require.Equal(t, "Composite", monitorDisplayName(monitor))
	require.Equal(t, PlatformComposite, monitor.GroupPlatform)
	require.Len(t, keys.createCalls, 1)
}

func TestChannelMonitorCreateCustomRejectsNonCompositeGroup(t *testing.T) {
	repo := &groupMonitorRepoStub{}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "OpenAI", PlatformOpenAI),
	}}
	keys := &groupMonitorKeyManagerStub{}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})

	monitor, err := svc.Create(context.Background(), groupMonitorCreateParams(MonitorProviderCustom, 1))

	require.Nil(t, monitor)
	require.ErrorIs(t, err, ErrChannelMonitorGroupPlatformMismatch)
	require.Empty(t, keys.createCalls)
	require.Empty(t, repo.created)
}

func TestChannelMonitorCreateCompensatesDedicatedKeyWhenPersistenceFails(t *testing.T) {
	persistErr := errors.New("database unavailable")
	repo := &groupMonitorRepoStub{createErr: persistErr}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "OpenAI", MonitorProviderOpenAI),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"orphan-candidate"}}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})

	monitor, err := svc.Create(context.Background(), groupMonitorCreateParams(MonitorProviderOpenAI, 1))

	require.Nil(t, monitor)
	require.ErrorIs(t, err, persistErr)
	require.Equal(t, []groupMonitorKeyDeleteCall{{rawKey: "orphan-candidate", userID: 9}}, keys.deleteCalls)
}

func TestChannelMonitorUpdateGroupPreservesCustomNameAndRotatesDedicatedKey(t *testing.T) {
	oldGroupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:              10,
		Name:            "Monitor",
		Provider:        MonitorProviderOpenAI,
		APIMode:         MonitorAPIModeChatCompletions,
		Endpoint:        "https://8.8.8.8",
		APIKey:          "enc:old-key",
		PrimaryModel:    "test-model",
		GroupID:         &oldGroupID,
		GroupName:       "Old Group",
		IntervalSeconds: 60,
		CreatedBy:       9,
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		2: activeMonitorGroup(2, "New Group", MonitorProviderOpenAI),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"new-key"}}
	encryptor := &groupMonitorEncryptor{}
	svc := newGroupMonitorService(repo, reader, keys, encryptor)
	newGroupID := int64(2)

	monitor, err := svc.Update(context.Background(), 10, ChannelMonitorUpdateParams{GroupID: &newGroupID})

	require.NoError(t, err)
	require.Equal(t, int64(2), *monitor.GroupID)
	require.Equal(t, "Monitor", monitor.Name)
	require.Equal(t, "New Group", monitor.GroupName)
	require.Empty(t, monitor.APIKey, "management response must not carry the rotated key")
	require.Len(t, repo.updated, 1)
	require.Equal(t, "enc:new-key", repo.updated[0].APIKey)
	require.Equal(t, []groupMonitorKeyCreateCall{{userID: 9, groupID: 2, monitorName: "Monitor"}}, keys.createCalls)
	require.Equal(t, []groupMonitorKeyDeleteCall{{rawKey: "old-key", userID: 9}}, keys.deleteCalls)
	require.Equal(t, []string{"enc:old-key"}, encryptor.decryptInputs)
}

func TestChannelMonitorUpdateGroupRefreshesInheritedName(t *testing.T) {
	oldGroupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:              10,
		Name:            "Old Group",
		Provider:        MonitorProviderOpenAI,
		Endpoint:        "https://8.8.8.8",
		APIKey:          "enc:old-key",
		PrimaryModel:    "test-model",
		GroupID:         &oldGroupID,
		GroupName:       "Old Group",
		IntervalSeconds: 60,
		CreatedBy:       9,
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		2: activeMonitorGroup(2, "New Group", MonitorProviderOpenAI),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"new-key"}}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})
	newGroupID := int64(2)
	submittedName := "Old Group"

	monitor, err := svc.Update(context.Background(), 10, ChannelMonitorUpdateParams{
		GroupID: &newGroupID,
		Name:    &submittedName,
	})

	require.NoError(t, err)
	require.Empty(t, monitor.Name)
	require.Equal(t, "New Group", monitorDisplayName(monitor))
	require.Equal(t, []groupMonitorKeyCreateCall{{userID: 9, groupID: 2, monitorName: "New Group"}}, keys.createCalls)
}

func TestChannelMonitorUpdateExplicitBlankNameFallsBackToGroupName(t *testing.T) {
	groupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:              10,
		Name:            "Custom name",
		Provider:        MonitorProviderOpenAI,
		Endpoint:        "https://8.8.8.8",
		APIKey:          "enc:managed-key",
		PrimaryModel:    "test-model",
		GroupID:         &groupID,
		GroupName:       "OpenAI Group",
		IntervalSeconds: 60,
		CreatedBy:       9,
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "OpenAI Group", MonitorProviderOpenAI),
	}}
	keys := &groupMonitorKeyManagerStub{}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})
	blankName := "  "

	monitor, err := svc.Update(context.Background(), 10, ChannelMonitorUpdateParams{Name: &blankName})

	require.NoError(t, err)
	require.Empty(t, monitor.Name)
	require.Equal(t, "OpenAI Group", monitorDisplayName(monitor))
	require.Empty(t, keys.createCalls)
}

func TestChannelMonitorUpdateExplicitCustomNameIsPreserved(t *testing.T) {
	groupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:              10,
		Name:            "OpenAI Group",
		Provider:        MonitorProviderOpenAI,
		Endpoint:        "https://8.8.8.8",
		APIKey:          "enc:managed-key",
		PrimaryModel:    "test-model",
		GroupID:         &groupID,
		GroupName:       "OpenAI Group",
		IntervalSeconds: 60,
		CreatedBy:       9,
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "OpenAI Group", MonitorProviderOpenAI),
	}}
	svc := newGroupMonitorService(repo, reader, &groupMonitorKeyManagerStub{}, &groupMonitorEncryptor{})
	customName := "  Renamed monitor  "

	monitor, err := svc.Update(context.Background(), 10, ChannelMonitorUpdateParams{Name: &customName})

	require.NoError(t, err)
	require.Equal(t, "Renamed monitor", monitor.Name)
}

func TestChannelMonitorUpdateCustomAcceptsCompositeGroup(t *testing.T) {
	oldGroupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:              10,
		Name:            "Custom monitor",
		Provider:        MonitorProviderCustom,
		Endpoint:        "https://8.8.8.8",
		APIKey:          "enc:old-key",
		PrimaryModel:    "test-model",
		GroupID:         &oldGroupID,
		GroupName:       "OpenAI Group",
		IntervalSeconds: 60,
		CreatedBy:       9,
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		2: activeMonitorGroup(2, "Custom Group", PlatformComposite),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"new-key"}}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})
	newGroupID := int64(2)

	monitor, err := svc.Update(context.Background(), 10, ChannelMonitorUpdateParams{GroupID: &newGroupID})

	require.NoError(t, err)
	require.Equal(t, "Custom monitor", monitor.Name)
	require.Equal(t, PlatformComposite, monitor.GroupPlatform)
	require.Equal(t, int64(2), *monitor.GroupID)
	require.Len(t, keys.createCalls, 1)
}

func TestChannelMonitorUpdateCanSwitchProviderToCustomAndSelectCompositeGroup(t *testing.T) {
	oldGroupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:              10,
		Name:            "OpenAI Group",
		Provider:        MonitorProviderOpenAI,
		Endpoint:        "https://8.8.8.8",
		APIKey:          "enc:old-key",
		PrimaryModel:    "test-model",
		GroupID:         &oldGroupID,
		GroupName:       "OpenAI Group",
		IntervalSeconds: 60,
		CreatedBy:       9,
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		2: activeMonitorGroup(2, "Custom Group", PlatformComposite),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"new-key"}}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})
	provider := MonitorProviderCustom
	newGroupID := int64(2)
	submittedName := "OpenAI Group"

	monitor, err := svc.Update(context.Background(), 10, ChannelMonitorUpdateParams{
		Provider: &provider,
		GroupID:  &newGroupID,
		Name:     &submittedName,
	})

	require.NoError(t, err)
	require.Equal(t, MonitorProviderCustom, monitor.Provider)
	require.Empty(t, monitor.Name)
	require.Equal(t, "Custom Group", monitorDisplayName(monitor))
	require.Equal(t, PlatformComposite, monitor.GroupPlatform)
	require.Equal(t, []groupMonitorKeyCreateCall{{userID: 9, groupID: 2, monitorName: "Custom Group"}}, keys.createCalls)
}

func TestChannelMonitorDeleteReclaimsDedicatedKey(t *testing.T) {
	groupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:        10,
		APIKey:    "enc:managed-key",
		GroupID:   &groupID,
		CreatedBy: 9,
	}}
	keys := &groupMonitorKeyManagerStub{}
	svc := newGroupMonitorService(repo, &groupMonitorReaderStub{}, keys, &groupMonitorEncryptor{})

	err := svc.Delete(context.Background(), 10)

	require.NoError(t, err)
	require.Equal(t, []int64{10}, repo.deleted)
	require.Equal(t, []groupMonitorKeyDeleteCall{{rawKey: "managed-key", userID: 9}}, keys.deleteCalls)
}

func TestChannelMonitorDeleteReclaimsDedicatedKeyAfterGroupDeleted(t *testing.T) {
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:        10,
		APIKey:    "enc:orphaned-managed-key",
		GroupID:   nil,
		CreatedBy: 9,
	}}
	keys := &groupMonitorKeyManagerStub{}
	svc := newGroupMonitorService(repo, &groupMonitorReaderStub{}, keys, &groupMonitorEncryptor{})

	err := svc.Delete(context.Background(), 10)

	require.NoError(t, err)
	require.Equal(t, []int64{10}, repo.deleted)
	require.Equal(t, []groupMonitorKeyDeleteCall{{rawKey: "orphaned-managed-key", userID: 9}}, keys.deleteCalls)
}

func TestChannelMonitorUpdateReclaimsDedicatedKeyAfterGroupDeleted(t *testing.T) {
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:              10,
		Name:            "Custom monitor",
		Provider:        MonitorProviderCustom,
		Endpoint:        "https://8.8.8.8",
		APIKey:          "enc:orphaned-managed-key",
		PrimaryModel:    "test-model",
		GroupID:         nil,
		IntervalSeconds: 60,
		CreatedBy:       9,
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		2: activeMonitorGroup(2, "Composite Group", PlatformComposite),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"replacement-key"}}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})
	groupID := int64(2)

	monitor, err := svc.Update(context.Background(), 10, ChannelMonitorUpdateParams{GroupID: &groupID})

	require.NoError(t, err)
	require.Empty(t, monitor.APIKey, "management response must not carry the replacement key")
	require.Equal(t, int64(2), *monitor.GroupID)
	require.Equal(t, []groupMonitorKeyDeleteCall{{rawKey: "orphaned-managed-key", userID: 9}}, keys.deleteCalls)
}

func TestChannelMonitorDuplicateCreatesIndependentDedicatedKey(t *testing.T) {
	groupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:               10,
		Name:             "Primary",
		Provider:         MonitorProviderOpenAI,
		APIMode:          MonitorAPIModeChatCompletions,
		Endpoint:         "https://8.8.8.8",
		APIKey:           "enc:source-key",
		PrimaryModel:     "test-model",
		GroupID:          &groupID,
		GroupName:        "OpenAI",
		IntervalSeconds:  60,
		CreatedBy:        9,
		BodyOverrideMode: MonitorBodyOverrideModeOff,
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "OpenAI", MonitorProviderOpenAI),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"copy-key"}}
	encryptor := &groupMonitorEncryptor{}
	svc := newGroupMonitorService(repo, reader, keys, encryptor)

	monitor, err := svc.Duplicate(context.Background(), 10, 22, "admin:22", "copy-once")

	require.NoError(t, err)
	require.Empty(t, monitor.APIKey, "management response must not carry the copy key")
	require.Len(t, repo.created, 1)
	require.Equal(t, "enc:copy-key", repo.created[0].APIKey)
	require.Equal(t, "Primary (Copy)", monitor.Name)
	require.Equal(t, []groupMonitorKeyCreateCall{{userID: 22, groupID: 1, monitorName: "Primary (Copy)"}}, keys.createCalls)
	require.Empty(t, encryptor.decryptInputs, "a group copy must not reuse the source monitor key")
}

func TestChannelMonitorDuplicateCustomAcceptsCrossPlatformGroup(t *testing.T) {
	groupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:                  10,
		Name:                "Custom monitor",
		Provider:            MonitorProviderCustom,
		Endpoint:            "https://8.8.8.8",
		APIKey:              "enc:source-key",
		PrimaryModel:        "test-model",
		GroupID:             &groupID,
		GroupName:           "Stale Group",
		GroupRateMultiplier: 9,
		GroupPlatform:       MonitorProviderOpenAI,
		IntervalSeconds:     60,
		CreatedBy:           9,
		BodyOverrideMode:    MonitorBodyOverrideModeOff,
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "Composite Group", PlatformComposite),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"copy-key"}}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})

	monitor, err := svc.Duplicate(context.Background(), 10, 22, "admin:22", "copy-custom")

	require.NoError(t, err)
	require.Equal(t, "Custom monitor (Copy)", monitor.Name)
	require.Equal(t, MonitorProviderCustom, monitor.Provider)
	require.Equal(t, "Composite Group", monitor.GroupName)
	require.Equal(t, 0.1, monitor.GroupRateMultiplier)
	require.Equal(t, PlatformComposite, monitor.GroupPlatform)
	require.Equal(t, []groupMonitorKeyCreateCall{{userID: 22, groupID: 1, monitorName: "Custom monitor (Copy)"}}, keys.createCalls)
}

func TestChannelMonitorDuplicateCompensatesDedicatedKeyWhenCloneFails(t *testing.T) {
	groupID := int64(1)
	repo := &groupMonitorRepoStub{existing: &ChannelMonitor{
		ID:           10,
		Name:         "Primary",
		Provider:     MonitorProviderOpenAI,
		APIKey:       "enc:source-key",
		GroupID:      &groupID,
		CreatedBy:    9,
		BodyOverride: map[string]any{"unsupported": make(chan int)},
	}}
	reader := &groupMonitorReaderStub{groups: map[int64]*Group{
		1: activeMonitorGroup(1, "OpenAI", MonitorProviderOpenAI),
	}}
	keys := &groupMonitorKeyManagerStub{nextKeys: []string{"copy-key"}}
	svc := newGroupMonitorService(repo, reader, keys, &groupMonitorEncryptor{})

	monitor, err := svc.Duplicate(context.Background(), 10, 22, "admin:22", "copy-invalid-body")

	require.Nil(t, monitor)
	require.Error(t, err)
	require.Empty(t, repo.created)
	require.Equal(t, []groupMonitorKeyDeleteCall{{rawKey: "copy-key", userID: 22}}, keys.deleteCalls)
}
