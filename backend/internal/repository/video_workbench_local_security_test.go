package repository

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoWorkbenchLocalRejectsOversizedContentAndCleansPartialFiles(t *testing.T) {
	directory := t.TempDir()
	storage := NewLocalVideoWorkbenchStorage(directory)
	storage.maxBytes = 1024
	id := "vidtask_" + strings.Repeat("b", 32)
	input := io.MultiReader(bytes.NewReader(videoMP4Fixture()), zeroVideoReader{})
	_, _, err := storage.Write(context.Background(), id, input, time.Minute)
	require.ErrorContains(t, err, "storage limit")
	_, err = os.Stat(filepath.Join(directory, id))
	require.ErrorIs(t, err, fs.ErrNotExist)

	content := append(videoMP4Fixture(), make([]byte, 1024-len(videoMP4Fixture()))...)
	size, _, err := storage.Write(context.Background(), id, bytes.NewReader(content), time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1024, size)
}

func TestVideoWorkbenchLocalDuplicateWritePreservesOriginal(t *testing.T) {
	storage := NewLocalVideoWorkbenchStorage(t.TempDir())
	ctx := context.Background()
	id := "vidtask_" + strings.Repeat("c", 32)
	original := append(videoMP4Fixture(), []byte("original")...)
	_, _, err := storage.Write(ctx, id, bytes.NewReader(original), time.Minute)
	require.NoError(t, err)
	_, _, err = storage.Write(ctx, id, bytes.NewReader(videoMP4Fixture()), time.Minute)
	require.ErrorIs(t, err, fs.ErrExist)
	file, err := storage.Open(ctx, id)
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	actual, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, original, actual)
}
