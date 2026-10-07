package setup

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin/binding"
)

func TestDecideAdminBootstrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		totalUsers int64
		adminUsers int64
		should     bool
		reason     string
	}{
		{
			name:       "empty database should create admin",
			totalUsers: 0,
			adminUsers: 0,
			should:     true,
			reason:     adminBootstrapReasonEmptyDatabase,
		},
		{
			name:       "admin exists should skip",
			totalUsers: 10,
			adminUsers: 1,
			should:     false,
			reason:     adminBootstrapReasonAdminExists,
		},
		{
			name:       "users exist without admin should skip",
			totalUsers: 5,
			adminUsers: 0,
			should:     false,
			reason:     adminBootstrapReasonUsersExistWithoutAdmin,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := decideAdminBootstrap(tc.totalUsers, tc.adminUsers)
			if got.shouldCreate != tc.should {
				t.Fatalf("shouldCreate=%v, want %v", got.shouldCreate, tc.should)
			}
			if got.reason != tc.reason {
				t.Fatalf("reason=%q, want %q", got.reason, tc.reason)
			}
		})
	}
}

func TestSetupDefaultAdminConcurrency(t *testing.T) {
	t.Run("simple mode admin uses higher concurrency", func(t *testing.T) {
		t.Setenv("RUN_MODE", "simple")
		if got := setupDefaultAdminConcurrency(); got != simpleModeAdminConcurrency {
			t.Fatalf("setupDefaultAdminConcurrency()=%d, want %d", got, simpleModeAdminConcurrency)
		}
	})

	t.Run("standard mode keeps existing default", func(t *testing.T) {
		t.Setenv("RUN_MODE", "standard")
		if got := setupDefaultAdminConcurrency(); got != defaultUserConcurrency {
			t.Fatalf("setupDefaultAdminConcurrency()=%d, want %d", got, defaultUserConcurrency)
		}
	})
}

func TestNeedsSetupSkipsWhenSkipSetupIsEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "true", value: "true"},
		{name: "one", value: "1"},
		{name: "yes", value: "yes"},
		{name: "trimmed mixed case true", value: "  TrUe  "},
		{name: "trimmed mixed case yes", value: "  YeS  "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATA_DIR", t.TempDir())
			t.Setenv("SKIP_SETUP", tc.value)

			if NeedsSetup() {
				t.Fatalf("NeedsSetup() = true, want false when SKIP_SETUP is enabled")
			}
		})
	}
}

func TestNeedsSetupFallsBackToFileDetectionWhenSkipSetupIsDisabled(t *testing.T) {
	tests := []struct {
		name         string
		skipSetupSet bool
		skipSetup    string
		markerFile   string
		want         bool
	}{
		{
			name: "unset without installation files",
			want: true,
		},
		{
			name:         "false without installation files",
			skipSetupSet: true,
			skipSetup:    " false ",
			want:         true,
		},
		{
			name:         "invalid value without installation files",
			skipSetupSet: true,
			skipSetup:    "enabled",
			want:         true,
		},
		{
			name:         "config file exists",
			skipSetupSet: true,
			skipSetup:    "false",
			markerFile:   ConfigFileName,
			want:         false,
		},
		{
			name:         "install lock file exists",
			skipSetupSet: true,
			skipSetup:    "invalid",
			markerFile:   InstallLockFile,
			want:         false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			t.Setenv("DATA_DIR", dataDir)
			if tc.skipSetupSet {
				t.Setenv("SKIP_SETUP", tc.skipSetup)
			} else {
				originalValue, wasSet := os.LookupEnv("SKIP_SETUP")
				if err := os.Unsetenv("SKIP_SETUP"); err != nil {
					t.Fatalf("Unsetenv(SKIP_SETUP) error = %v", err)
				}
				t.Cleanup(func() {
					if wasSet {
						_ = os.Setenv("SKIP_SETUP", originalValue)
						return
					}
					_ = os.Unsetenv("SKIP_SETUP")
				})
			}

			if tc.markerFile != "" {
				if err := os.WriteFile(filepath.Join(dataDir, tc.markerFile), nil, 0o600); err != nil {
					t.Fatalf("WriteFile(%s) error = %v", tc.markerFile, err)
				}
			}

			if got := NeedsSetup(); got != tc.want {
				t.Fatalf("NeedsSetup() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSetupMigrationTimeout(t *testing.T) {
	t.Run("uses default timeout when unset", func(t *testing.T) {
		cfg := &SetupConfig{}
		if got := cfg.migrationTimeout(); got != 60*time.Second {
			t.Fatalf("migrationTimeout()=%s, want 60s", got)
		}
	})

	t.Run("uses configured timeout", func(t *testing.T) {
		cfg := &SetupConfig{MigrationTimeoutSeconds: 300}
		if got := cfg.migrationTimeout(); got != 300*time.Second {
			t.Fatalf("migrationTimeout()=%s, want 300s", got)
		}
	})
}

func TestWriteConfigFileKeepsDefaultUserConcurrency(t *testing.T) {
	t.Setenv("RUN_MODE", "simple")
	t.Setenv("DATA_DIR", t.TempDir())

	if err := writeConfigFile(&SetupConfig{}); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	data, err := os.ReadFile(GetConfigFilePath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !strings.Contains(string(data), "user_concurrency: 5") {
		t.Fatalf("config missing default user concurrency, got:\n%s", string(data))
	}
}

func TestWriteConfigFileIncludesRedisUsername(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())

	if err := writeConfigFile(&SetupConfig{
		Redis: RedisConfig{
			Host:     "redis",
			Port:     6379,
			Username: "app-user",
		},
	}); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	data, err := os.ReadFile(GetConfigFilePath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !strings.Contains(string(data), "username: app-user") {
		t.Fatalf("config missing Redis username, got:\n%s", string(data))
	}
}

func TestBuildDatabaseConnectionDSNsUsesPostgresForBootstrap(t *testing.T) {
	cfg := &DatabaseConfig{
		Host:     "db",
		Port:     5432,
		User:     "sub2api",
		Password: "secret",
		DBName:   "sub2api",
		SSLMode:  "disable",
	}

	bootstrapDSN, targetDSN := buildDatabaseConnectionDSNs(cfg)

	if !strings.Contains(bootstrapDSN, "dbname=postgres") {
		t.Fatalf("bootstrap DSN = %q, want default postgres database", bootstrapDSN)
	}
	if strings.Contains(bootstrapDSN, "dbname=sub2api") {
		t.Fatalf("bootstrap DSN = %q, should not connect to target database before checking/creating it", bootstrapDSN)
	}
	if !strings.Contains(targetDSN, "dbname=sub2api") {
		t.Fatalf("target DSN = %q, want configured database", targetDSN)
	}
}

func TestPrepareAdminCredentialsGeneratesMissingValues(t *testing.T) {
	t.Parallel()

	admin := AdminConfig{Email: "  ", Password: ""}
	emailGenerated, passwordGenerated, err := prepareAdminCredentials(&admin)
	if err != nil {
		t.Fatalf("prepareAdminCredentials() error = %v", err)
	}
	if !emailGenerated || !passwordGenerated {
		t.Fatalf("generated flags = (%v, %v), want (true, true)", emailGenerated, passwordGenerated)
	}
	if !regexp.MustCompile(`^admin-[0-9a-f]{12}@sub2api\.local$`).MatchString(admin.Email) {
		t.Fatalf("generated email = %q, want admin-<12 hex>@sub2api.local", admin.Email)
	}
	loginReq := struct {
		Email string `binding:"required,email"`
	}{Email: admin.Email}
	if err := binding.Validator.ValidateStruct(&loginReq); err != nil {
		t.Fatalf("generated email rejected by login validator: %v", err)
	}
	if len(admin.Password) != 32 {
		t.Fatalf("generated password length = %d, want 32", len(admin.Password))
	}

	other := AdminConfig{Password: " \t "}
	if _, _, err := prepareAdminCredentials(&other); err != nil {
		t.Fatalf("prepareAdminCredentials() second call error = %v", err)
	}
	if other.Email == admin.Email || other.Password == admin.Password {
		t.Fatal("generated credentials should be random")
	}
}

func TestPrepareAdminCredentialsPreservesValidPasswords(t *testing.T) {
	t.Parallel()

	for _, password := range []string{
		"12345678",
		"a-strong-password",
		" password with spaces ",
		strings.Repeat("a", 72),
		strings.Repeat("\u00e9", 36),
	} {
		t.Run(password, func(t *testing.T) {
			t.Parallel()

			admin := AdminConfig{Email: "  owner@example.com\n", Password: password}
			emailGenerated, passwordGenerated, err := prepareAdminCredentials(&admin)
			if err != nil {
				t.Fatalf("prepareAdminCredentials() error = %v", err)
			}
			if emailGenerated || passwordGenerated {
				t.Fatalf("generated flags = (%v, %v), want (false, false)", emailGenerated, passwordGenerated)
			}
			if admin.Email != "owner@example.com" || admin.Password != password {
				t.Fatal("provided credentials should be preserved except email whitespace")
			}
			user := service.User{}
			if err := user.SetPassword(admin.Password); err != nil {
				t.Fatalf("accepted password cannot be hashed: %v", err)
			}
		})
	}
}

func TestPrepareAdminCredentialsRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		email    string
		password string
		wantErr  string
	}{
		{name: "short password", email: "owner@example.com", password: "123456", wantErr: "invalid admin password"},
		{name: "seven bytes", email: "owner@example.com", password: "1234567", wantErr: "invalid admin password"},
		{name: "bcrypt limit", email: "owner@example.com", password: strings.Repeat("a", 73), wantErr: "invalid admin password"},
		{name: "former limit", email: "owner@example.com", password: strings.Repeat("a", 128), wantErr: "invalid admin password"},
		{name: "multibyte over limit", email: "owner@example.com", password: strings.Repeat("\u00e9", 37), wantErr: "invalid admin password"},
		{name: "missing domain", email: "admin", password: "a-strong-password", wantErr: "invalid admin email"},
		{name: "unqualified domain", email: "a@b", password: "a-strong-password", wantErr: "invalid admin email"},
		{name: "display name", email: "Owner <owner@example.com>", password: "a-strong-password", wantErr: "invalid admin email"},
		{name: "bracketed address", email: "<owner@example.com>", password: "a-strong-password", wantErr: "invalid admin email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			admin := AdminConfig{Email: tt.email, Password: tt.password}
			_, _, err := prepareAdminCredentials(&admin)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("prepareAdminCredentials() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func expectAdminBootstrapCounts(mock sqlmock.Sqlmock, totalUsers, adminUsers int64) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(1) FROM users")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(totalUsers))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(1) FROM users WHERE role = $1")).
		WithArgs(service.RoleAdmin).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(adminUsers))
}

func TestBootstrapAdminUserSkipsValidationWhenNotCreating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		totalUsers int64
		adminUsers int64
		reason     string
	}{
		{name: "admin exists", totalUsers: 3, adminUsers: 1, reason: adminBootstrapReasonAdminExists},
		{name: "users exist without admin", totalUsers: 3, adminUsers: 0, reason: adminBootstrapReasonUsersExistWithoutAdmin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer func() { _ = db.Close() }()
			expectAdminBootstrapCounts(mock, tt.totalUsers, tt.adminUsers)

			cfg := &SetupConfig{Admin: AdminConfig{Email: "admin", Password: "123456"}}
			created, reason, err := bootstrapAdminUser(context.Background(), db, cfg)
			if err != nil || created || reason != tt.reason {
				t.Fatalf("bootstrapAdminUser() = (%v, %q, %v), want (false, %q, nil)", created, reason, err, tt.reason)
			}
			if cfg.Admin.Email != "admin" || cfg.Admin.Password != "123456" {
				t.Fatal("skipped bootstrap should not modify credentials")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unexpected database interaction: %v", err)
			}
		})
	}
}

func TestBootstrapAdminUserRejectsInvalidCredentialsWithoutInsert(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		admin   AdminConfig
		wantErr string
	}{
		{name: "weak password", admin: AdminConfig{Password: "123456"}, wantErr: "invalid admin password"},
		{name: "invalid email", admin: AdminConfig{Email: "a@b", Password: "a-strong-password"}, wantErr: "invalid admin email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New() error = %v", err)
			}
			defer func() { _ = db.Close() }()
			expectAdminBootstrapCounts(mock, 0, 0)

			// An unexpected INSERT returns a different error and fails this assertion.
			created, _, err := bootstrapAdminUser(context.Background(), db, &SetupConfig{Admin: tt.admin})
			if err == nil || created || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("bootstrapAdminUser() = (%v, %v), want %q", created, err, tt.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unexpected database interaction: %v", err)
			}
		})
	}
}

func TestBootstrapAdminUserCreatesAdminWithGeneratedCredentials(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer func() { _ = db.Close() }()
	expectAdminBootstrapCounts(mock, 0, 0)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO users")).
		WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), service.RoleAdmin, sqlmock.AnyArg(),
			sqlmock.AnyArg(), service.StatusActive, sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	cfg := &SetupConfig{}
	created, reason, err := bootstrapAdminUser(context.Background(), db, cfg)
	if err != nil || !created || reason != adminBootstrapReasonEmptyDatabase {
		t.Fatalf("bootstrapAdminUser() = (%v, %q, %v), want (true, %q, nil)", created, reason, err, adminBootstrapReasonEmptyDatabase)
	}
	if !regexp.MustCompile(`^admin-[0-9a-f]{12}@sub2api\.local$`).MatchString(cfg.Admin.Email) {
		t.Fatalf("admin email = %q, want generated admin-<12 hex>@sub2api.local", cfg.Admin.Email)
	}
	if len(cfg.Admin.Password) != 32 {
		t.Fatalf("admin password length = %d, want generated 32", len(cfg.Admin.Password))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations not met: %v", err)
	}
}
