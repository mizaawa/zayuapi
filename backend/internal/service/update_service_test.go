//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
	latestRepo     string
	recentRepo     string
	download       func(context.Context, string, string, int64) error
	checksum       func(context.Context, string) ([]byte, error)
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	s.latestRepo = repo
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(_ context.Context, repo string, _ int) ([]*GitHubRelease, error) {
	s.recentRepo = repo
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(ctx context.Context, url, dest string, maxSize int64) error {
	if s.download != nil {
		return s.download(ctx, url, dest, maxSize)
	}
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(ctx context.Context, url string) ([]byte, error) {
	if s.checksum != nil {
		return s.checksum(ctx, url)
	}
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServiceKeepsPrivateAssetAPILinksInCache(t *testing.T) {
	cache := &updateServiceCacheStub{}
	client := &updateServiceGitHubClientStub{release: &GitHubRelease{
		TagName: "v0.1.148",
		Assets: []GitHubAsset{{Name: "archive.tar.gz", BrowserDownloadURL: "https://github.com/mizaawa/zayuapi/releases/download/v0.1.148/archive.tar.gz",
			APIURL: "https://api.github.com/repos/mizaawa/zayuapi/releases/assets/123"}},
	}}
	svc := NewUpdateService(cache, client, "0.1.147", "release")
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, client.release.Assets[0].BrowserDownloadURL, info.ReleaseInfo.Assets[0].DownloadURL)
	require.Equal(t, client.release.Assets[0].APIURL, info.ReleaseInfo.Assets[0].APIURL)
	cached, err := svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.Equal(t, info.ReleaseInfo.Assets, cached.ReleaseInfo.Assets)
}

func TestUpdateServicePrivateUpdateAndRollbackUseAssetAPILinks(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "update", true: "rollback"}[rollback], func(t *testing.T) {
			checksumErr := errors.New("stop before replacing executable")
			client := &updateServiceGitHubClientStub{}
			svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.147", "release")
			archiveName := "sub2api_0.1.148_" + svc.getArchiveName() + ".tar.gz"
			assets := []GitHubAsset{
				{Name: archiveName, BrowserDownloadURL: "https://github.com/mizaawa/zayuapi/releases/download/v0.1.148/" + archiveName,
					APIURL: "https://api.github.com/repos/mizaawa/zayuapi/releases/assets/123"},
				{Name: "checksums.txt", BrowserDownloadURL: "https://github.com/mizaawa/zayuapi/releases/download/v0.1.148/checksums.txt",
					APIURL: "https://api.github.com/repos/mizaawa/zayuapi/releases/assets/124"},
			}
			client.release = &GitHubRelease{TagName: "v0.1.148", Assets: assets}
			client.recentReleases = []*GitHubRelease{{TagName: "v0.1.146", Assets: assets}}
			client.download = func(_ context.Context, url, dest string, maxSize int64) error {
				require.Equal(t, assets[0].APIURL, url)
				require.Equal(t, archiveName, filepath.Base(dest))
				require.EqualValues(t, maxDownloadSize, maxSize)
				return os.WriteFile(dest, []byte("archive"), 0600)
			}
			client.checksum = func(_ context.Context, url string) ([]byte, error) {
				require.Equal(t, assets[1].APIURL, url)
				return nil, checksumErr
			}
			var err error
			if rollback {
				err = svc.RollbackToVersion(context.Background(), "0.1.146")
			} else {
				err = svc.PerformUpdate(context.Background())
			}
			require.ErrorIs(t, err, checksumErr)
		})
	}
}

func TestUpdateAssetDownloadURLSupportsOldCache(t *testing.T) {
	asset := Asset{DownloadURL: "https://github.com/mizaawa/zayuapi/releases/download/v1/archive.tar.gz"}
	require.Equal(t, asset.DownloadURL, asset.downloadURL())
	asset.APIURL = "https://api.github.com/repos/mizaawa/zayuapi/releases/assets/123"
	require.Equal(t, asset.APIURL, asset.downloadURL())
	require.NoError(t, validateDownloadURL(asset.APIURL))
	require.NoError(t, validateDownloadURL("https://release-assets.githubusercontent.com/archive"))
	require.Error(t, validateDownloadURL("https://user:password@api.github.com/repos/mizaawa/zayuapi/releases/assets/123"))
}

func TestUpdateServiceLegacyCacheBelongsToPrivateRepository(t *testing.T) {
	for _, repo := range []string{"mizaawa/zayuapi", "mizaawa/sub2api", "Wei-Shaw/sub2api"} {
		t.Run(repo, func(t *testing.T) {
			data, err := json.Marshal(struct {
				Latest      string       `json:"latest"`
				ReleaseInfo *ReleaseInfo `json:"release_info"`
				Timestamp   int64        `json:"timestamp"`
			}{Latest: "0.1.148", ReleaseInfo: &ReleaseInfo{HTMLURL: "https://github.com/" + repo + "/releases/tag/v0.1.148"}, Timestamp: time.Now().Unix()})
			require.NoError(t, err)
			svc := NewUpdateService(&updateServiceCacheStub{data: string(data)}, &updateServiceGitHubClientStub{}, "0.1.147", "release")
			info, err := svc.getFromCache(context.Background())
			if repo == githubRepo {
				require.NoError(t, err)
				require.True(t, info.Cached)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	client := &updateServiceGitHubClientStub{
		release: &GitHubRelease{
			TagName: "v0.1.132",
			Name:    "v0.1.132",
		},
	}
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		client,
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
	require.Equal(t, "mizaawa/zayuapi", client.latestRepo)
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceRollbackUsesMaintainedRepository(t *testing.T) {
	client := &updateServiceGitHubClientStub{
		recentReleases: []*GitHubRelease{
			{TagName: "v0.1.146"},
		},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.147", "release")

	_, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Equal(t, "mizaawa/zayuapi", client.recentRepo)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}
