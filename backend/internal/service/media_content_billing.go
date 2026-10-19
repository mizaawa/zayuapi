package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

type mediaTaskContentBillingKey struct{}

type mediaTaskContentOwner struct {
	taskID        string
	userID, keyID int64
	groupID       int64
}

// Only the authenticated gateway handler can attach this owner. An upstream
// task ID or a client header cannot authorize a different user's reservation.
func WithMediaTaskContentBilling(ctx context.Context, taskID string, userID, keyID int64, groupID *int64) context.Context {
	return context.WithValue(ctx, mediaTaskContentBillingKey{}, mediaTaskContentOwner{strings.TrimSpace(taskID), userID, keyID, derefGroupID(groupID)})
}

func (s *OpenAIGatewayService) settleMediaContentDelivery(ctx context.Context, owner mediaTaskContentOwner) error {
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil
	}
	repo, ok := s.usageBillingRepo.(MediaBillingRepository)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	r, err := repo.FindMediaTask(ctx, owner.taskID, owner.userID, owner.keyID)
	if err != nil {
		return err
	}
	if r == nil {
		// Legacy tasks were billed at submission. Their account binding is
		// still checked by the handler; do not charge them a second time.
		return nil
	}
	if r.GroupID != owner.groupID || r.Quote.Command.UserID != owner.userID || r.Quote.Command.APIKeyID != owner.keyID {
		return ErrUsageBillingRequestConflict
	}
	quote := r.Quote
	if r.VideoOutcome != nil {
		quote = *r.VideoOutcome
	}
	// StageMediaOutcome and SettleMedia share the refund terminal lock. A
	// winning refund makes settlement fail before a single content byte is
	// published. A winning final quote permanently prevents a later refund.
	return s.settleMediaQuote(ctx, r, &quote)
}

// Complete the upstream read on disk before committing either a charge or a
// client response. A truncated upstream stream, JSON error, or empty response
// cannot become a paid delivery. Range responses with actual bytes are paid
// deliveries of the generation; HEAD/empty responses are not.
func (s *OpenAIGatewayService) writeMediaTaskContentResponse(ctx context.Context, c *gin.Context, resp *http.Response) error {
	owner, guarded := ctx.Value(mediaTaskContentBillingKey{}).(mediaTaskContentOwner)
	if !guarded || ctx.Value(videoWorkbenchContextKey{}) == true {
		return writeGrokMediaContentResponse(c, resp)
	}
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("video content response is incomplete")
	}
	if c.Request != nil && c.Request.Method == http.MethodHead || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		copyResponse := *resp
		copyResponse.Body = http.NoBody
		return writeGrokMediaContentResponse(c, &copyResponse)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || isVideoContentJSONResponse(resp) {
		return fmt.Errorf("video content response is not a successful file")
	}
	limit := config.DefaultUpstreamResponseReadMaxBytes
	if s.cfg != nil && s.cfg.Gateway.UpstreamResponseReadMaxBytes > 0 {
		limit = s.cfg.Gateway.UpstreamResponseReadMaxBytes
	}
	file, err := os.CreateTemp("", "zayu-video-delivery-*")
	if err != nil {
		return fmt.Errorf("prepare video delivery: %w", err)
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	n, err := io.Copy(file, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("read video delivery: %w", err)
	}
	if n == 0 || n > limit {
		return fmt.Errorf("video delivery is empty or exceeds the response limit")
	}
	expected := resp.ContentLength
	if raw := strings.TrimSpace(resp.Header.Get("Content-Length")); raw != "" {
		expected, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || expected < 0 {
			return fmt.Errorf("invalid video content length")
		}
		if expected != n {
			return io.ErrUnexpectedEOF
		}
	} else if expected > 0 && expected != n {
		return io.ErrUnexpectedEOF
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := s.settleMediaContentDelivery(ctx, owner); err != nil {
		return fmt.Errorf("settle video delivery: %w", err)
	}
	copyResponse := *resp
	copyResponse.Body, copyResponse.ContentLength = file, n
	return writeGrokMediaContentResponse(c, &copyResponse)
}
