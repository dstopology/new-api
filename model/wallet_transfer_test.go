package model

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestWalletMigrationIdentityReplay(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&WalletTransfer{}, &WalletMigrationAccount{}, &Task{}, &Midjourney{}))
	previousMode := WalletMigrationEnabled
	WalletMigrationEnabled = true
	t.Cleanup(func() { WalletMigrationEnabled = previousMode })
	hash, err := common.Password2Hash("wallet-test-password")
	require.NoError(t, err)
	create := func() User {
		name := "wm-" + uuid.NewString()[:16]
		user := User{Username: name, Email: name + "@test.invalid", Password: hash, AffCode: name, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 5000000}
		require.NoError(t, DB.Create(&user).Error)
		t.Cleanup(func() { DB.Where("user_id = ?", user.Id).Delete(&WalletTransfer{}); DB.Unscoped().Delete(&user) })
		return user
	}
	a, b := create(), create()
	inspected, err := InspectWalletForMigration(a.Email, "wallet-test-password")
	require.NoError(t, err)
	require.Equal(t, a.Id, inspected.Id)
	var untouched User
	require.NoError(t, DB.First(&untouched, a.Id).Error)
	require.Equal(t, 5000000, untouched.Quota)

	id := uuid.NewString()
	_, err = TransferWalletForMigration(context.Background(), id, b.Id, a.Username, "wallet-test-password")
	require.ErrorContains(t, err, "user changed")
	first, err := TransferWalletForMigration(context.Background(), id, a.Id, a.Username, "wallet-test-password")
	require.NoError(t, err)
	repeat, err := TransferWalletForMigration(context.Background(), id, a.Id, a.Email, "wallet-test-password")
	require.NoError(t, err)
	require.Equal(t, first.TransferID, repeat.TransferID)
	_, err = TransferWalletForMigration(context.Background(), id, b.Id, b.Username, "wallet-test-password")
	require.ErrorContains(t, err, "another user")

	WalletMigrationEnabled = false
	_, err = TransferWalletForMigration(context.Background(), uuid.NewString(), b.Id, b.Username, "wallet-test-password")
	require.ErrorContains(t, err, "wallet_migration_not_enabled")
	repeat, err = TransferWalletForMigration(context.Background(), id, a.Id, a.Username, "wallet-test-password")
	require.NoError(t, err)
	require.Equal(t, first.TransferID, repeat.TransferID)
	WalletMigrationEnabled = true

	require.NoError(t, DB.Model(&User{}).Where("id = ?", a.Id).Update("quota", 2500000).Error)
	next, err := TransferWalletForMigration(context.Background(), uuid.NewString(), a.Id, a.Username, "wallet-test-password")
	require.NoError(t, err)
	require.Equal(t, 2500000, next.Amount)
	require.NotEqual(t, first.TransferID, next.TransferID)
	zero, err := TransferWalletForMigration(context.Background(), uuid.NewString(), a.Id, a.Username, "wallet-test-password")
	require.NoError(t, err)
	require.Zero(t, zero.Amount)
}
