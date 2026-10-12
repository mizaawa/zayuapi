package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoWorkbenchReferenceURLs(t *testing.T) {
	policy := service.VideoWorkbenchRuntime{MaxReferenceImages: 1, MaxReferenceVideos: 1, MaxReferenceAudios: 1}
	for _, raw := range []string{
		"https://cdn.example.com/image.png?signature=private",
		"http://cdn.example.com/video.mp4",
	} {
		payload := map[string]any{
			"model": "videos-mini-480p", "prompt": "waves", "duration": 4,
			"referenceImages": []string{raw}, "referenceVideos": []string{raw}, "referenceAudios": []string{raw},
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		parsed, err := parseVideoWorkbenchPayload(body, policy)
		require.NoError(t, err)
		require.Equal(t, []string{raw}, parsed.ReferenceVideos)
	}
	for _, raw := range []string{
		"https://127.0.0.1/a", "http://169.254.169.254/a", "https://10.0.0.1/a",
		"https://[::1]/a", "https://[::ffff:127.0.0.1]/a", "https://localhost./a",
		"https://user:secret@cdn.example.com/a", "https://cdn.example.com/a#fragment",
		"file:///etc/passwd", "//cdn.example.com/a", "javascript:alert(1)",
		"https://cdn.example.com:99999/a", "https://cdn.example.com/" + strings.Repeat("a", 8192),
	} {
		t.Run(raw[:min(len(raw), 60)], func(t *testing.T) {
			for _, kind := range []string{"image", "video", "audio"} {
				require.False(t, validVideoWorkbenchReference(raw, kind))
			}
		})
	}
}
