package repository

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPgRestoreCommandRequiresAtomicErrorHandling(t *testing.T) {
	dumper := &PgDumper{cfg: &config.DatabaseConfig{Host: "localhost", Port: 5432, DBName: "app", User: "app"}}
	cmd := dumper.restoreCommand(context.Background())
	require.Contains(t, cmd.Args, "--no-psqlrc")
	require.Contains(t, cmd.Args, "--set=ON_ERROR_STOP=1")
	require.Contains(t, cmd.Args, "--single-transaction")
	// psql only applies --single-transaction with -c or -f, including stdin via -f -.
	require.Contains(t, cmd.Args, "--file=-")
}

func TestPrepareRestoreFilePreservesSnapshot(t *testing.T) {
	dump := "-- dump\nCOPY public.usage_counters FROM stdin;\n0001-01-01\t123\n\\.\n"
	file, err := prepareRestoreFile(strings.NewReader(dump))
	require.NoError(t, err)
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	postlude, err := restoreCountersPostlude()
	require.NoError(t, err)
	require.Equal(t, restoreCountersPrelude+dump+postlude, string(data))
	require.Contains(t, postlude, "IF NOT EXISTS (SELECT 1 FROM schema_migrations")
}

func TestPrepareRestoreFileRejectsCorruptDownloadBeforeRestore(t *testing.T) {
	for _, mode := range []string{"checksum", "truncated", "empty"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TMPDIR", dir)
			t.Setenv("TMP", dir)
			t.Setenv("TEMP", dir)
			var zipped bytes.Buffer
			writer := gzip.NewWriter(&zipped)
			_, err := io.WriteString(writer, "CREATE TABLE complete_statement (id int);\n")
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			data := zipped.Bytes()
			switch mode {
			case "checksum":
				data[len(data)-8] ^= 1
			case "truncated":
				data = data[:len(data)-4]
			}
			reader, err := gzip.NewReader(bytes.NewReader(data))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, reader.Close()) })
			var source io.Reader = reader
			if mode == "empty" {
				source = strings.NewReader("")
			}
			file, err := prepareRestoreFile(source)
			require.Error(t, err)
			require.Nil(t, file)
			if mode == "checksum" {
				require.True(t, errors.Is(err, gzip.ErrChecksum))
			}
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Empty(t, entries, "failed downloads must not leave temporary database dumps")
		})
	}
}

func TestPgRestoreOutputIsBounded(t *testing.T) {
	output := &pgRestoreOutput{}
	data := bytes.Repeat([]byte("x"), 128*1024)
	n, err := output.Write(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	n, err = output.Write(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	require.Equal(t, 64*1024, output.Len())
	_, err = io.Copy(output, bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, 64*1024, output.Len())
}
