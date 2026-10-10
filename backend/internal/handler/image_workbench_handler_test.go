package handler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageWorkbenchMetadataValidation(t *testing.T) {
	_, err := parseImageWorkbenchMetadata("application/json", []byte(`{"model":"gpt-image-2","prompt":"`+strings.Repeat("\u753b", 32000)+`"}`))
	require.NoError(t, err)
	for _, count := range []string{"1", "3", "5", "10", "20"} {
		metadata, err := parseImageWorkbenchMetadata("application/json", []byte(`{"model":"gpt-image-2","prompt":"test","n":`+count+`}`))
		require.NoError(t, err)
		require.Equal(t, "auto", metadata.Quality)
	}
	for _, payload := range []string{
		`{}`, `{"model":"gpt-image-2","prompt":" "}`,
		`{"model":"gpt-image-2","prompt":"test","n":2}`,
		`{"model":"gpt-image-2","prompt":"test","quality":"invalid"}`,
		`{"model":"gpt-image-2","prompt":"test","images":[{},{},{}]}`,
		`{"model":"gpt-image-2","prompt":"` + strings.Repeat("a", 32001) + `"}`,
	} {
		_, err := parseImageWorkbenchMetadata("application/json", []byte(payload))
		require.Error(t, err)
	}
	_, err = parseImageWorkbenchMetadata("multipart/form-data; boundary=test", []byte(`{}`))
	require.Error(t, err)
}
