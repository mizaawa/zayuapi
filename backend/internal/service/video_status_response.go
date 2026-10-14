package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/tidwall/gjson"
)

// Some New API relays prepend their recovery error to an otherwise complete
// task response. Recover only that exact envelope for the task being queried.
func recoverNewAPIVideoStatus(body []byte, taskID string) ([]byte, bool) {
	if taskID == "" {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var failure, task json.RawMessage
	if decoder.Decode(&failure) != nil || gjson.GetBytes(failure, "error.type").String() != "new_api_panic" || decoder.Decode(&task) != nil {
		return nil, false
	}
	var extra json.RawMessage
	if decoder.Decode(&extra) != io.EOF || gjson.GetBytes(task, "error").Exists() || gjson.GetBytes(task, "object").String() != "video" || gjson.GetBytes(task, "id").String() != taskID {
		return nil, false
	}
	if id := gjson.GetBytes(task, "task_id"); id.Exists() && id.String() != taskID {
		return nil, false
	}
	switch gjson.GetBytes(task, "status").String() {
	case "queued", "pending", "in_progress", "processing", "completed", "succeeded", "success", "done", "failed", "failure", "error", "cancelled", "canceled", "expired":
		return task, true
	default:
		return nil, false
	}
}

func (s *OpenAIGatewayService) normalizeCompatibleVideoStatusResponse(account *Account, taskID string, resp *http.Response) error {
	if account == nil || account.Platform != PlatformCustom || account.MediaProtocol() == "8yes" || resp == nil || resp.Body == nil || resp.StatusCode != http.StatusInternalServerError {
		return nil
	}
	body, err := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return err
	}
	if normalized, ok := recoverNewAPIVideoStatus(body, taskID); ok {
		resp.Body = io.NopCloser(bytes.NewReader(normalized))
		resp.StatusCode = http.StatusOK
		resp.Status = "200 OK"
		resp.ContentLength = int64(len(normalized))
		resp.TransferEncoding = nil
		if resp.Header == nil {
			resp.Header = make(http.Header)
		}
		resp.Header.Set("Content-Type", "application/json")
		resp.Header.Set("Content-Length", strconv.Itoa(len(normalized)))
		resp.Header.Del("Transfer-Encoding")
	}
	return nil
}
