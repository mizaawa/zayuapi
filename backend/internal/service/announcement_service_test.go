package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type announcementRepoStub struct {
	item  *Announcement
	items []Announcement
}

func (s *announcementRepoStub) Create(_ context.Context, a *Announcement) error {
	s.item = a
	return nil
}

func (s *announcementRepoStub) GetByID(_ context.Context, _ int64) (*Announcement, error) {
	if s.item == nil {
		return nil, ErrAnnouncementNotFound
	}
	return s.item, nil
}

func (s *announcementRepoStub) Update(_ context.Context, a *Announcement) error {
	s.item = a
	return nil
}

func (s *announcementRepoStub) TogglePin(_ context.Context, _ int64) (*AnnouncementPinResult, error) {
	if s.item == nil {
		return nil, ErrAnnouncementNotFound
	}
	s.item.IsPinned = !s.item.IsPinned
	return &AnnouncementPinResult{Announcement: s.item}, nil
}

func (*announcementRepoStub) Delete(context.Context, int64) error {
	return nil
}

func (*announcementRepoStub) List(context.Context, pagination.PaginationParams, AnnouncementListFilters) ([]Announcement, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (s *announcementRepoStub) ListActive(context.Context, time.Time) ([]Announcement, error) {
	return s.items, nil
}

type announcementPinUserRepo struct{ UserRepository }

func (*announcementPinUserRepo) GetByID(context.Context, int64) (*User, error) {
	return &User{ID: 1}, nil
}

type announcementPinSubscriptionRepo struct{ UserSubscriptionRepository }

func (*announcementPinSubscriptionRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

type announcementPinReadRepo struct{ AnnouncementReadRepository }

func (*announcementPinReadRepo) GetReadMapByUser(context.Context, int64, []int64) (map[int64]time.Time, error) {
	return map[int64]time.Time{1: time.Now()}, nil
}

func TestAnnouncementListForUserKeepsReadPinBeforeUnread(t *testing.T) {
	repo := &announcementRepoStub{items: []Announcement{
		{ID: 3, Status: AnnouncementStatusActive},
		{ID: 2, Status: AnnouncementStatusActive},
		{ID: 1, Status: AnnouncementStatusActive, IsPinned: true},
	}}
	svc := NewAnnouncementService(repo, &announcementPinReadRepo{}, &announcementPinUserRepo{}, &announcementPinSubscriptionRepo{})
	items, err := svc.ListForUser(context.Background(), 1, false)
	require.NoError(t, err)
	require.Len(t, items, 3)
	require.Equal(t, int64(1), items[0].Announcement.ID)
	require.NotNil(t, items[0].ReadAt)
	require.Equal(t, int64(3), items[1].Announcement.ID)
	require.Equal(t, int64(2), items[2].Announcement.ID)
}

func TestAnnouncementServiceCreateRejectsEqualStartEndTimes(t *testing.T) {
	repo := &announcementRepoStub{}
	svc := NewAnnouncementService(repo, nil, nil, nil)
	now := time.Unix(1776790020, 0)

	_, err := svc.Create(context.Background(), &CreateAnnouncementInput{
		Title:      "公告",
		Content:    "内容",
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModePopup,
		StartsAt:   &now,
		EndsAt:     &now,
	})
	require.ErrorIs(t, err, ErrAnnouncementInvalidSchedule)
}

func TestAnnouncementServiceUpdateRejectsEqualStartEndTimes(t *testing.T) {
	repo := &announcementRepoStub{
		item: &Announcement{
			ID:         1,
			Title:      "公告",
			Content:    "内容",
			Status:     AnnouncementStatusActive,
			NotifyMode: AnnouncementNotifyModePopup,
		},
	}
	svc := NewAnnouncementService(repo, nil, nil, nil)
	now := time.Unix(1776790020, 0)
	startsAt := &now
	endsAt := &now

	_, err := svc.Update(context.Background(), 1, &UpdateAnnouncementInput{
		StartsAt: &startsAt,
		EndsAt:   &endsAt,
	})
	require.ErrorIs(t, err, ErrAnnouncementInvalidSchedule)
}
