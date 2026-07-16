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
