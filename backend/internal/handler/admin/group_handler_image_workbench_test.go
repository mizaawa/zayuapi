//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workbenchGroupAdminServiceStub struct {
	service.AdminService
	created *service.CreateGroupInput
	updated *service.UpdateGroupInput
}

func (s *workbenchGroupAdminServiceStub) CreateGroup(_ context.Context, input *service.CreateGroupInput) (*service.Group, error) {
	s.created = input
	return &service.Group{ID: 9, ImageWorkbenchEnabled: input.ImageWorkbenchEnabled}, nil
}

func (s *workbenchGroupAdminServiceStub) UpdateGroup(_ context.Context, _ int64, input *service.UpdateGroupInput) (*service.Group, error) {
	s.updated = input
	group := &service.Group{ID: 9, ImageWorkbenchEnabled: true}
	if input.ImageWorkbenchEnabled != nil {
		group.ImageWorkbenchEnabled = *input.ImageWorkbenchEnabled
	}
	return group, nil
}

func TestGroupHandlerImageWorkbenchDeclaration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{http.MethodPost, http.MethodPut} {
		for _, value := range []string{"true", "false", ""} {
			t.Run(method+"/"+value, func(t *testing.T) {
				svc := &workbenchGroupAdminServiceStub{}
				router := gin.New()
				handler := NewGroupHandler(svc, nil, nil)
				router.POST("/groups/9", handler.Create)
				router.PUT("/groups/:id", handler.Update)
				body := `{"name":"image-group","platform":"openai","rate_multiplier":1`
				if value != "" {
					body += `,"image_workbench_enabled":` + value
				}
				request := httptest.NewRequest(method, "/groups/9", strings.NewReader(body+"}"))
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				want := value == "true"
				if method == http.MethodPost {
					require.Equal(t, want, svc.created.ImageWorkbenchEnabled)
				} else if value == "" {
					require.Nil(t, svc.updated.ImageWorkbenchEnabled)
					want = true
				} else {
					require.NotNil(t, svc.updated.ImageWorkbenchEnabled)
					require.Equal(t, want, *svc.updated.ImageWorkbenchEnabled)
				}
				var response struct {
					Data struct {
						Enabled *bool `json:"image_workbench_enabled"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
				require.NotNil(t, response.Data.Enabled)
				require.Equal(t, want, *response.Data.Enabled)
			})
		}
	}
}
