package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestBackupWebDAVConfigEncryptsRedactsAndKeepsPassword(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	ctx := context.Background()

	got, err := svc.UpdateWebDAVConfig(ctx, BackupWebDAVConfig{
		Enabled: true, URL: "https://dav.example.test/dav", Username: "backup", Password: "secret", Path: "database/backups",
	})
	require.NoError(t, err)
	require.True(t, got.PasswordConfigured)
	require.Empty(t, got.Password)

	raw, err := repo.GetValue(ctx, settingKeyBackupWebDAVConfig)
	require.NoError(t, err)
	require.NotContains(t, raw, `"password":"secret"`)
	var stored BackupWebDAVConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.Equal(t, "ENC:secret", stored.Password)

	readback, err := svc.GetWebDAVConfig(ctx)
	require.NoError(t, err)
	require.True(t, readback.PasswordConfigured)
	require.Empty(t, readback.Password)
	require.Equal(t, "database/backups", readback.Path)

	_, err = svc.UpdateWebDAVConfig(ctx, BackupWebDAVConfig{
		Enabled: true, URL: "https://dav.example.test/dav", Username: "backup", Path: "database/archive",
	})
	require.NoError(t, err)
	raw, err = repo.GetValue(ctx, settingKeyBackupWebDAVConfig)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.Equal(t, "ENC:secret", stored.Password, "omitted password retains encrypted storage value")
}

func TestBackupWebDAVConfigRejectsNewSecretWithoutFixedEncryptionKey(t *testing.T) {
	svc := newTestBackupServiceEphemeralKey(newMockSettingRepo())
	_, err := svc.UpdateWebDAVConfig(context.Background(), BackupWebDAVConfig{
		URL: "https://dav.example.test", Username: "backup", Password: "secret",
	})
	require.ErrorIs(t, err, ErrSecretEncryptionKeyNotConfigured)
}

func TestBackupWebDAVTestConnectionUsesSavedPasswordWhenOmitted(t *testing.T) {
	repo := newMockSettingRepo()
	var built *BackupS3Config
	cfg := &config.Config{Totp: config.TotpConfig{EncryptionKeyConfigured: true}}
	factory := func(_ context.Context, got *BackupS3Config) (BackupObjectStore, error) {
		copy := *got
		built = &copy
		return newMockObjectStore(), nil
	}
	svc := NewBackupService(repo, cfg, &plainEncryptor{}, factory, &mockDumper{})
	_, err := svc.UpdateWebDAVConfig(context.Background(), BackupWebDAVConfig{
		URL: "https://dav.example.test", Username: "backup", Password: "stored-secret", Path: "store",
	})
	require.NoError(t, err)

	err = svc.TestWebDAVConnection(context.Background(), BackupWebDAVConfig{
		URL: "https://dav.example.test", Username: "backup", Path: "store",
	})
	require.NoError(t, err)
	require.NotNil(t, built)
	require.Equal(t, "webdav", built.StorageType)
	require.Equal(t, "stored-secret", built.WebDAVPassword)
	require.Equal(t, "store", built.WebDAVPath)
}

func TestBackupUsesEnabledWebDAVForUploadRestoreDownloadAndDelete(t *testing.T) {
	repo := newMockSettingRepo()
	dumper := &mockDumper{dumpData: []byte("postgres dump")}
	webdavStore := newMockObjectStore()
	s3Store := newMockObjectStore()
	var selected []string
	cfg := &config.Config{
		Database: config.DatabaseConfig{DBName: "testdb"},
		Totp:     config.TotpConfig{EncryptionKeyConfigured: true},
	}
	factory := func(_ context.Context, storage *BackupS3Config) (BackupObjectStore, error) {
		selected = append(selected, storage.StorageType)
		if storage.StorageType == "webdav" {
			return webdavStore, nil
		}
		return s3Store, nil
	}
	svc := NewBackupService(repo, cfg, &plainEncryptor{}, factory, dumper)
	_, err := svc.UpdateS3Config(context.Background(), BackupS3Config{
		Bucket: "bucket", AccessKeyID: "access", SecretAccessKey: "s3-secret",
	})
	require.NoError(t, err)
	_, err = svc.UpdateWebDAVConfig(context.Background(), BackupWebDAVConfig{
		Enabled: true, URL: "https://dav.example.test/dav", Username: "backup", Password: "dav-secret", Path: "database/backups",
	})
	require.NoError(t, err)

	record, err := svc.CreateBackup(context.Background(), "manual", 14)
	require.NoError(t, err)
	require.Equal(t, "completed", record.Status)
	require.Equal(t, "webdav", record.StorageType)
	require.NotContains(t, record.S3Key, "database/backups", "the adapter applies the configured path once")
	require.NotEmpty(t, webdavStore.objects[record.S3Key])
	require.Empty(t, s3Store.objects)

	// Turning WebDAV off selects S3 for future backups, while existing WebDAV
	// records continue using their own provider for download, restore and delete.
	_, err = svc.UpdateWebDAVConfig(context.Background(), BackupWebDAVConfig{
		Enabled: false, URL: "https://dav.example.test/dav", Username: "backup", Path: "database/backups",
	})
	require.NoError(t, err)
	gotRecord, body, err := svc.OpenBackupDownload(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, record.ID, gotRecord.ID)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.NotEmpty(t, data)

	require.NoError(t, svc.RestoreBackup(context.Background(), record.ID))
	require.Equal(t, []byte("postgres dump"), dumper.restored)
	require.Contains(t, selected, "webdav")
	require.NotContains(t, selected, "s3", "the old backup must not be routed to the newly selected S3 backend")

	require.NoError(t, svc.DeleteBackup(context.Background(), record.ID))
	require.Empty(t, webdavStore.objects)
}

func TestBackupActiveWebDAVRequiresCredentials(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{dumpData: []byte("dump")}, newMockObjectStore())
	_, err := svc.UpdateWebDAVConfig(context.Background(), BackupWebDAVConfig{Enabled: true, URL: "https://dav.example.test"})
	require.NoError(t, err)
	_, err = svc.CreateBackup(context.Background(), "manual", 14)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "WebDAV"))
}
