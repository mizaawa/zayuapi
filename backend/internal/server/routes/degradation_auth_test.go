package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type degradationAuthUsers struct {
	service.UserRepository
	users map[int64]*service.User
}

func (r *degradationAuthUsers) GetByID(_ context.Context, id int64) (*service.User, error) {
	if user, ok := r.users[id]; ok {
		return user, nil
	}
	return nil, errors.New("user not found")
}
func (*degradationAuthUsers) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}
func (*degradationAuthUsers) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	return nil
}

type degradationAuthSettings struct {
	service.DegradationSettingsStore
	enabled bool
}

func (s degradationAuthSettings) GetValue(_ context.Context, key string) (string, error) {
	switch key {
	case service.SettingKeyDegradationMonitorEnabled:
		if s.enabled {
			return "true", nil
		}
	case service.SettingKeyDegradationMonitorConfig:
		return `{"max_display_per_group":2}`, nil
	}
	return "", nil
}

type degradationAuthRepo struct {
	service.DegradationMonitorRepository
	reads    int
	monitors map[int64]*service.DegradationMonitor
	runs     map[int64]*service.DegradationRun
}

func (r *degradationAuthRepo) ListMonitors(context.Context) ([]*service.DegradationMonitor, error) {
	r.reads++
	return []*service.DegradationMonitor{r.monitors[1], r.monitors[2], r.monitors[3]}, nil
}
func (r *degradationAuthRepo) ListRecentFinishedRuns(_ context.Context, monitorIDs []int64, limit int) (map[int64][]*service.DegradationRun, error) {
	r.reads++
	result := map[int64][]*service.DegradationRun{}
	for _, monitorID := range monitorIDs {
		var runs []*service.DegradationRun
		for _, run := range r.runs {
			if run.MonitorID == monitorID && run.Status != service.DegradationRunStatusRunning {
				summary := *run
				summary.Prompt, summary.Output = "", ""
				runs = append(runs, &summary)
			}
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i].ID > runs[j].ID })
		if len(runs) > limit {
			runs = runs[:limit]
		}
		result[monitorID] = runs
	}
	return result, nil
}
func (r *degradationAuthRepo) GetRun(_ context.Context, id int64) (*service.DegradationRun, error) {
	r.reads++
	if run, ok := r.runs[id]; ok {
		return run, nil
	}
	return nil, service.ErrDegradationRunNotFound
}
func (r *degradationAuthRepo) GetMonitor(_ context.Context, id int64) (*service.DegradationMonitor, error) {
	r.reads++
	if monitor, ok := r.monitors[id]; ok {
		return monitor, nil
	}
	return nil, service.ErrDegradationMonitorNotFound
}

// Exercise production routes, JWT validation, role checks and DTO serialization.
// Persistence uses synthetic fixtures; requests never contact a live deployment.
func TestDegradationRoutesPublicVisibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			users := &degradationAuthUsers{users: map[int64]*service.User{
				1: {ID: 1, Role: service.RoleUser, Status: service.StatusActive},
				2: {ID: 2, Role: service.RoleAdmin, Status: service.StatusActive},
			}}
			cfg := &config.Config{}
			cfg.JWT.Secret = "degradation-route-test-secret-only"
			cfg.JWT.AccessTokenExpireMinutes = 60
			auth := service.NewAuthService(nil, users, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
			userSvc := service.NewUserService(users, nil, nil, nil)
			tokens := map[int64]string{}
			for id, user := range users.users {
				token, err := auth.GenerateToken(context.Background(), user)
				require.NoError(t, err)
				tokens[id] = token
			}
			repo := &degradationAuthRepo{
				monitors: map[int64]*service.DegradationMonitor{
					1: {ID: 1, Name: "public-monitor", GroupName: "public-group", Enabled: true},
					2: {ID: 2, Name: "hidden-monitor", Enabled: false},
					3: {ID: 3, Name: "deleted-group-monitor", Enabled: true, GroupDeleted: true},
				},
				runs: map[int64]*service.DegradationRun{},
			}
			for _, id := range []int64{100, 513, 514, 515, 516, 517} {
				started := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
				repo.runs[id] = &service.DegradationRun{
					ID: id, MonitorID: 1, Status: service.DegradationRunStatusSuccess,
					Prompt: "gallery-prompt", Output: "<svg>gallery-output</svg>",
					ErrorMessage: "private-upstream-error", InputTokens: 123, OutputTokens: 456,
					StartedAt: started, FinishedAt: &started,
				}
			}
			repo.runs[100].Prompt = "archived-prompt"
			repo.runs[100].Output = "archived-output"
			repo.runs[513].Status = service.DegradationRunStatusFailed
			repo.runs[515].Status = service.DegradationRunStatusRunning
			repo.runs[516].MonitorID = 2
			repo.runs[517].MonitorID = 3
			svc := service.NewDegradationMonitorService(repo, degradationAuthSettings{enabled: enabled}, nil, nil)
			handlers := &handler.Handlers{
				Degradation: handler.NewDegradationUserHandler(svc),
				Admin:       &handler.AdminHandlers{Degradation: adminhandler.NewDegradationMonitorHandler(svc)},
			}
			router := gin.New()
			audit := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
			RegisterUserRoutes(router.Group("/api/v1"), handlers,
				middleware.NewJWTAuthMiddleware(auth, userSvc, nil, nil), audit, nil, nil)
			RegisterAdminRoutes(router.Group("/api/v1"), handlers,
				middleware.NewAdminAuthMiddleware(auth, userSvc, nil, nil), audit,
				middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }), nil, nil)

			for _, endpoint := range []struct {
				path   string
				status int
			}{
				{"/api/v1/degradation/overview", 200},
				{"/api/v1/degradation/runs/514", 200},
				{"/api/v1/degradation/runs/513", 200},
				{"/api/v1/degradation/runs/100", 404},
				{"/api/v1/degradation/runs/515", 404},
				{"/api/v1/degradation/runs/516", 404},
				{"/api/v1/degradation/runs/517", 404},
				{"/api/v1/degradation/runs/99999", 404},
				{"/api/v1/degradation/runs/invalid", 400},
				{"/api/v1/degradation/runs/0", 400},
				{"/api/v1/admin/degradation/runs/100", 200},
			} {
				for _, identity := range []struct {
					name, token string
					admin       bool
				}{
					{name: "anonymous"},
					{name: "invalid_jwt", token: "invalid-jwt"},
					{name: "ordinary_user", token: tokens[1]},
					{name: "admin", token: tokens[2], admin: true},
				} {
					t.Run(endpoint.path+"/"+identity.name, func(t *testing.T) {
						repo.reads = 0
						request := httptest.NewRequest(http.MethodGet, endpoint.path, nil)
						if identity.token != "" {
							request.Header.Set("Authorization", "Bearer "+identity.token)
						}
						recorder := httptest.NewRecorder()
						router.ServeHTTP(recorder, request)
						t.Logf("GET %s identity=%s status=%d body=%s repository_reads=%d", endpoint.path, identity.name, recorder.Code, recorder.Body.String(), repo.reads)
						expected := endpoint.status
						isAdminEndpoint := endpoint.path == "/api/v1/admin/degradation/runs/100"
						if !enabled && expected == 200 && endpoint.path != "/api/v1/degradation/overview" && !isAdminEndpoint {
							expected = 404
						}
						if identity.name == "anonymous" || identity.name == "invalid_jwt" {
							expected = 401
						} else if isAdminEndpoint && !identity.admin {
							expected = 403
						}
						assert.Equal(t, expected, recorder.Code)
						if expected == 401 || expected == 403 {
							assert.Zero(t, repo.reads)
						}
						if expected == 404 {
							assert.JSONEq(t, `{"code":404,"message":"degradation result not found","reason":"DEGRADATION_RUN_NOT_FOUND"}`, recorder.Body.String())
						}
						if expected != 200 {
							return
						}
						if isAdminEndpoint {
							assert.Contains(t, recorder.Body.String(), "archived-prompt")
							assert.Contains(t, recorder.Body.String(), "archived-output")
							return
						}
						for _, private := range []string{"error_message", "private-upstream-error", "input_tokens", "output_tokens", "cost", "api_key", "prompt_id", "group_id", "created_by", "trigger_source", "archived-prompt", "archived-output"} {
							assert.NotContains(t, recorder.Body.String(), private)
						}
						var body struct {
							Data map[string]json.RawMessage `json:"data"`
						}
						require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
						if endpoint.path == "/api/v1/degradation/overview" {
							var monitors []struct {
								Runs []struct {
									ID int64 `json:"id"`
								} `json:"runs"`
							}
							require.NoError(t, json.Unmarshal(body.Data["monitors"], &monitors))
							if enabled {
								require.Len(t, monitors, 1)
								require.Len(t, monitors[0].Runs, 2)
								assert.Equal(t, int64(514), monitors[0].Runs[0].ID)
								assert.Equal(t, int64(513), monitors[0].Runs[1].ID)
							} else {
								assert.Empty(t, monitors)
							}
							assert.NotContains(t, recorder.Body.String(), `"prompt":`)
							assert.NotContains(t, recorder.Body.String(), `"output":`)
						} else {
							assert.JSONEq(t, `"gallery-prompt"`, string(body.Data["prompt"]))
							if endpoint.path == "/api/v1/degradation/runs/514" {
								assert.JSONEq(t, `"<svg>gallery-output</svg>"`, string(body.Data["output"]))
							} else {
								assert.NotContains(t, body.Data, "output", "failed partial output stays private")
							}
						}
					})
				}
			}
			t.Run("anonymous_preview", func(t *testing.T) {
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/degradation/preview-frame", nil))
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Header().Get("Content-Security-Policy"), "sandbox allow-scripts")
			})
		})
	}
}
