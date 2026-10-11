//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoWorkbenchCustomOnlyGroupAndAuthSnapshot(t *testing.T) {
	ctx := context.Background()
	for _, platform := range []string{PlatformCustom, PlatformOpenAI, PlatformGrok, PlatformAnthropic} {
		repo := &groupRepoStubForAdmin{}
		svc := &adminServiceImpl{groupRepo: repo}
		group, err := svc.CreateGroup(ctx, &CreateGroupInput{Name: platform, Platform: platform, RateMultiplier: 1, VideoWorkbenchEnabled: true})
		require.NoError(t, err)
		require.Equal(t, platform == PlatformCustom, group.VideoWorkbenchEnabled)
		repo.getByID = group
		for _, enabled := range []bool{false, true} {
			group, err = svc.UpdateGroup(ctx, group.ID, &UpdateGroupInput{VideoWorkbenchEnabled: &enabled})
			require.NoError(t, err)
			require.Equal(t, enabled && platform == PlatformCustom, group.VideoWorkbenchEnabled)
		}
	}
	svc := &APIKeyService{}
	for _, enabled := range []bool{true, false} {
		key := &APIKey{ID: 1, User: &User{ID: 2}, Group: &Group{ID: 3, Platform: PlatformCustom, VideoWorkbenchEnabled: enabled}}
		snapshot := svc.snapshotFromAPIKey(ctx, key)
		data, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
		require.NoError(t, err)
		var entry APIKeyAuthCacheEntry
		require.NoError(t, json.Unmarshal(data, &entry))
		restored, used, err := svc.applyAuthCacheEntry("sk-fixture", &entry)
		require.NoError(t, err)
		require.True(t, used)
		require.Equal(t, enabled, restored.Group.VideoWorkbenchEnabled)
	}
}

func TestVideoWorkbenchCustomLongDurationUsageAndBilling(t *testing.T) {
	for _, seconds := range []int{30, 60, 123} {
		t.Run(strconv.Itoa(seconds), func(t *testing.T) {
			const groupID int64 = 729
			const model = "videos-standard-720p"
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			users := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(logs, users, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = newCustomVideoChannelPricingResolverForTest(groupID, model, BillingModeVideo, .1)
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{RequestID: "video-test-" + strconv.Itoa(seconds), Model: model, BillingModel: model, VideoCount: 1, VideoDurationSeconds: seconds, VideoPriceMultiplier: 1.3, Duration: time.Second},
				APIKey: &APIKey{ID: 101, GroupID: i64p(groupID), Group: &Group{ID: groupID, Platform: PlatformCustom, VideoRateIndependent: true, VideoRateMultiplier: .5}},
				User:   &User{ID: 201}, Account: &Account{ID: 301, Platform: PlatformCustom},
			})
			require.NoError(t, err)
			require.NotNil(t, logs.lastLog)
			require.NotNil(t, logs.lastLog.VideoDurationSeconds)
			require.Equal(t, seconds, *logs.lastLog.VideoDurationSeconds)
			require.InDelta(t, float64(seconds)*.1, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, float64(seconds)*.1*.5*1.3, logs.lastLog.ActualCost, 1e-12)
			require.InDelta(t, logs.lastLog.ActualCost, users.lastAmount, 1e-12)
			require.Equal(t, 1, users.deductCalls)
		})
	}
}

type videoWorkbenchBrokenReader struct{}

func (videoWorkbenchBrokenReader) Read([]byte) (int, error) {
	return 0, errors.New("stream interrupted")
}

func TestVideoWorkbenchGatewayDownloadCompletionAndCancellation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, broken := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		reader := io.Reader(strings.NewReader("partial-video"))
		if broken {
			reader = io.MultiReader(reader, videoWorkbenchBrokenReader{})
		}
		err := writeGrokMediaContentResponse(c, &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: -1, Body: io.NopCloser(reader)})
		require.Equal(t, broken, err != nil)
		require.Equal(t, !broken, c.GetBool(GrokMediaContentCompleteKey))
	}
	ctx, cancel := context.WithCancel(WithVideoWorkbenchContext(context.Background()))
	upstream, release := videoMediaUpstreamContext(ctx)
	defer release()
	cancel()
	require.ErrorIs(t, upstream.Err(), context.Canceled)
}
