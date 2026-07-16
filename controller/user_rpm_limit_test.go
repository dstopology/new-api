package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildUserManagementViewProtectsGroupRpmLimits(t *testing.T) {
	user := &model.User{Id: 7, Username: "limited-user", RpmLimits: `{"default":42}`}

	hiddenJson, err := common.Marshal(buildUserManagementView(user, false))
	require.NoError(t, err)
	var hidden map[string]any
	require.NoError(t, common.Unmarshal(hiddenJson, &hidden))
	require.NotContains(t, hidden, "rpm_limits")

	visibleJson, err := common.Marshal(buildUserManagementView(user, true))
	require.NoError(t, err)
	var visible map[string]any
	require.NoError(t, common.Unmarshal(visibleJson, &visible))
	visibleLimits, ok := visible["rpm_limits"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(42), visibleLimits["default"])
}

func TestCanManageUserRpmRequiresDesignatedRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		username string
		role     int
		allowed  bool
	}{
		{name: "designated root", username: common.SecuritySettingsOwnerUsername, role: common.RoleRootUser, allowed: true},
		{name: "different root", username: "other-root", role: common.RoleRootUser, allowed: false},
		{name: "same username admin", username: common.SecuritySettingsOwnerUsername, role: common.RoleAdminUser, allowed: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Set("username", test.username)
			context.Set("role", test.role)
			require.Equal(t, test.allowed, canManageUserRpm(context))
		})
	}
}

func TestUpdateUserGroupRpmLimitRejectsDifferentRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/user/7/rpm_limits", nil)
	context.Params = gin.Params{{Key: "id", Value: "7"}}
	context.Set("username", "other-root")
	context.Set("role", common.RoleRootUser)

	UpdateUserGroupRpmLimit(context)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])
}
