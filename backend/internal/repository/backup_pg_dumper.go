package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
)

// PgDumper implements service.DBDumper using pg_dump/psql
type PgDumper struct {
	cfg *config.DatabaseConfig
}

// NewPgDumper creates a new PgDumper
func NewPgDumper(cfg *config.Config) service.DBDumper {
	return &PgDumper{cfg: &cfg.Database}
}

// Dump executes pg_dump and returns a streaming reader of the output
func (d *PgDumper) Dump(ctx context.Context) (io.ReadCloser, error) {
	args := []string{
		"-h", d.cfg.Host,
		"-p", fmt.Sprintf("%d", d.cfg.Port),
		"-U", d.cfg.User,
		"-d", d.cfg.DBName,
		"--no-owner",
		"--no-acl",
		"--clean",
		"--if-exists",
	}

	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	if d.cfg.Password != "" {
		cmd.Env = append(cmd.Environ(), "PGPASSWORD="+d.cfg.Password)
	}
	if d.cfg.SSLMode != "" {
		cmd.Env = append(cmd.Environ(), "PGSSLMODE="+d.cfg.SSLMode)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start pg_dump: %w", err)
	}

	// 返回一个 ReadCloser：读 stdout，关闭时等待进程退出
	return &cmdReadCloser{ReadCloser: stdout, cmd: cmd}, nil
}

// Restore validates the input stream on disk before starting the restore transaction.
func (d *PgDumper) Restore(ctx context.Context, data io.Reader) error {
	file, err := prepareRestoreFile(data)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()

	cmd := d.restoreCommand(ctx)
	cmd.Stdin = file
	cmd.Stdout = io.Discard
	output := &pgRestoreOutput{}
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, output.String())
	}
	return nil
}

func (d *PgDumper) restoreCommand(ctx context.Context) *exec.Cmd {
	args := []string{
		"-h", d.cfg.Host,
		"-p", fmt.Sprintf("%d", d.cfg.Port),
		"-U", d.cfg.User,
		"-d", d.cfg.DBName,
		"--no-psqlrc",
		"--set=ON_ERROR_STOP=1",
		"--quiet",
		"--single-transaction",
		"--file=-",
	}

	cmd := exec.CommandContext(ctx, "psql", args...)
	if d.cfg.Password != "" {
		cmd.Env = append(cmd.Environ(), "PGPASSWORD="+d.cfg.Password)
	}
	if d.cfg.SSLMode != "" {
		cmd.Env = append(cmd.Environ(), "PGSSLMODE="+d.cfg.SSLMode)
	}

	return cmd
}

const persistentCountersMigration = "213_persistent_usage_counters.sql"

// Older dumps do not know about the function's dependency on the usage_logs row type.
// The dump recreates it, or the compatibility migration below does so before commit.
const restoreCountersPrelude = `
DO $restore_counters$
BEGIN
    IF to_regclass('public.usage_logs') IS NOT NULL THEN
        LOCK TABLE public.usage_logs IN ACCESS EXCLUSIVE MODE;
        DROP FUNCTION IF EXISTS public.accumulate_usage_counters(public.usage_logs, bigint);
    END IF;
END;
$restore_counters$;
`

func restoreCountersPostlude() (string, error) {
	migration, err := migrations.FS.ReadFile(persistentCountersMigration)
	if err != nil {
		return "", fmt.Errorf("read counter migration: %w", err)
	}
	content := strings.TrimSpace(string(migration))
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	counters := fmt.Sprintf(`
SET LOCAL search_path TO public;
SET LOCAL standard_conforming_strings = on;
DO $restore_counters$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = '%s') THEN
        -- A pre-counter backup has no historical totals. Discard leftovers from
        -- the target database and begin counting new activity after restoration.
        DROP TABLE IF EXISTS usage_counters, usage_counter_refunds, dashboard_user_financials;
        EXECUTE '%s';
        INSERT INTO schema_migrations (filename, checksum) VALUES ('%s', '%s');
    END IF;
    IF to_regclass('usage_counters') IS NULL
        OR to_regclass('usage_counter_refunds') IS NULL
        OR to_regclass('dashboard_user_financials') IS NULL
        OR to_regprocedure('accumulate_usage_counters(usage_logs,bigint)') IS NULL
        OR (SELECT COUNT(*) FROM pg_trigger WHERE tgenabled IN ('O', 'A') AND (
            (tgrelid = 'usage_logs'::regclass AND tgname IN ('usage_counters_mark', 'usage_counters_write'))
            OR (tgrelid = 'users'::regclass AND tgname = 'dashboard_balance_write')
            OR (tgrelid = 'redeem_codes'::regclass AND tgname IN ('dashboard_recharge_mark', 'dashboard_recharge_write'))
        )) <> 5 THEN
        RAISE EXCEPTION 'Backup is missing persistent statistics tables, functions or triggers';
    END IF;
END;
$restore_counters$;
`, persistentCountersMigration, strings.ReplaceAll(content, "'", "''"), persistentCountersMigration, checksum)
	media, err := restoreMediaBillingPostlude()
	return counters + media, err
}

func restoreMediaBillingPostlude() (string, error) {
	var out strings.Builder
	for _, filename := range []string{"214_media_precharge.sql", "215_media_billing_outcomes.sql", "216_media_payment_lifecycle.sql"} {
		migration, err := migrations.FS.ReadFile(filename)
		if err != nil {
			return "", fmt.Errorf("read media billing migration: %w", err)
		}
		content := strings.TrimSpace(string(migration))
		checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		cleanup := ""
		switch filename {
		case "214_media_precharge.sql":
			// Precharge records belong to the restored balances, never to the
			// database that happened to occupy this server before restoration.
			cleanup = "DROP TABLE IF EXISTS media_billing_events; DROP TABLE IF EXISTS media_billing_reservations;"
		case "216_media_payment_lifecycle.sql":
			cleanup = "DROP TABLE IF EXISTS media_billing_events;"
		}
		fmt.Fprintf(&out, `
DO $restore_media$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = '%s') THEN
        %s
        EXECUTE '%s';
        INSERT INTO schema_migrations (filename, checksum) VALUES ('%s', '%s');
    END IF;
END;
$restore_media$;
`, filename, cleanup, strings.ReplaceAll(content, "'", "''"), filename, checksum)
	}
	_, _ = out.WriteString(`
DO $restore_media$
BEGIN
    IF to_regclass('media_billing_reservations') IS NULL
        OR NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='media_billing_reservations' AND column_name='final_quote')
        OR NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='media_billing_reservations' AND column_name='refund_requested') THEN
        RAISE EXCEPTION 'Backup is missing media billing reservation data';
    END IF;
    IF to_regclass('media_billing_events') IS NULL
        OR NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='media_billing_reservations' AND column_name='dispatch_started_at') THEN
        RAISE EXCEPTION 'Backup is missing media billing lifecycle data';
    END IF;
END;
$restore_media$;
`)
	return out.String(), nil
}

func prepareRestoreFile(data io.Reader) (*os.File, error) {
	postlude, err := restoreCountersPostlude()
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "sub2api-restore-*.sql")
	if err != nil {
		return nil, fmt.Errorf("create restore file: %w", err)
	}
	ready := false
	defer func() {
		if !ready {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}()
	if _, err := io.WriteString(file, restoreCountersPrelude); err != nil {
		return nil, fmt.Errorf("write restore prelude: %w", err)
	}
	// Reading to EOF verifies gzip checksums and download errors before psql can
	// commit a truncated stream. Only the copy buffer, not the dump, lives in memory.
	size, err := io.Copy(file, data)
	if err != nil {
		return nil, fmt.Errorf("read backup: %w", err)
	}
	if size == 0 {
		return nil, fmt.Errorf("backup is empty")
	}
	if _, err := io.WriteString(file, postlude); err != nil {
		return nil, fmt.Errorf("write restore postlude: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind restore file: %w", err)
	}
	ready = true
	return file, nil
}

// Bound diagnostics even for a large restore with many PostgreSQL notices.
type pgRestoreOutput struct{ buffer bytes.Buffer }

func (b *pgRestoreOutput) Len() int       { return b.buffer.Len() }
func (b *pgRestoreOutput) String() string { return b.buffer.String() }

func (b *pgRestoreOutput) Write(p []byte) (int, error) {
	const limit = 64 * 1024
	if remaining := limit - b.Len(); remaining > 0 {
		_, _ = b.buffer.Write(p[:min(len(p), remaining)])
	}
	return len(p), nil
}

// cmdReadCloser wraps a command stdout pipe and waits for the process on Close
type cmdReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *cmdReadCloser) Close() error {
	// Close the pipe first
	_ = c.ReadCloser.Close()
	// Wait for the process to exit
	if err := c.cmd.Wait(); err != nil {
		return fmt.Errorf("pg_dump exited with error: %w", err)
	}
	return nil
}
