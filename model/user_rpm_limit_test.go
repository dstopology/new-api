package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestUpdateUserGroupRpmLimit(t *testing.T) {
	truncateTables(t)

	user := &User{
		Id:       998_003,
		Username: "rpm-limit-user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, UpdateUserGroupRpmLimit(user.Id, "default", 240))
	require.NoError(t, UpdateUserGroupRpmLimit(user.Id, "vip", 60))

	var updatedUser User
	require.NoError(t, DB.Select("rpm_limits").First(&updatedUser, "id = ?", user.Id).Error)
	require.Equal(t, map[string]int{"default": 240, "vip": 60}, updatedUser.GetRpmLimits())

	require.NoError(t, UpdateUserGroupRpmLimit(user.Id, "default", 0))
	require.NoError(t, DB.Select("rpm_limits").First(&updatedUser, "id = ?", user.Id).Error)
	require.Equal(t, map[string]int{"vip": 60}, updatedUser.GetRpmLimits())
}

func TestFormatUserLogsHidesLegacyUserRpmLimitMarker(t *testing.T) {
	logs := []*Log{
		{
			Id:      99,
			Content: "请求失败，状态码 429，错误码 user_rpm_limit",
			Other:   `{"failed":true,"status_code":429,"error_type":"rate_limit_error","error_code":"user_rpm_limit","error_message":"Selected model is at capacity."}`,
		},
	}

	formatUserLogs(logs, 0)

	require.Equal(t, 1, logs[0].Id)
	require.Equal(t, "请求失败，状态码 429", logs[0].Content)
	require.NotContains(t, logs[0].Other, "user_rpm_limit")
	require.NotContains(t, logs[0].Other, "rate_limit_error")

	var other struct {
		Failed     bool   `json:"failed"`
		StatusCode int    `json:"status_code"`
		ErrorType  string `json:"error_type"`
		ErrorCode  string `json:"error_code"`
	}
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	require.True(t, other.Failed)
	require.Equal(t, 429, other.StatusCode)
	require.Equal(t, "server_error", other.ErrorType)
	require.Empty(t, other.ErrorCode)
}
