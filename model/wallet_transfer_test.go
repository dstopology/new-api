package model

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestWalletMigrationBalanceProofAndIdentityReplay(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&WalletTransfer{}, &WalletMigrationAccount{}, &Task{}, &Midjourney{}))
	previousMode, previousQuotaPerUnit := WalletMigrationEnabled, common.QuotaPerUnit
	WalletMigrationEnabled, common.QuotaPerUnit = true, 500_000
	t.Cleanup(func() { WalletMigrationEnabled, common.QuotaPerUnit = previousMode, previousQuotaPerUnit })
	create := func() User {
		name := "wm-" + uuid.NewString()[:16]
		user := User{Username: name, Email: name + "@test.invalid", Password: "unused", AffCode: name, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 5_000_000}
		require.NoError(t, DB.Create(&user).Error)
		t.Cleanup(func() { DB.Where("user_id = ?", user.Id).Delete(&WalletTransfer{}); DB.Unscoped().Delete(&user) })
		return user
	}
	a, b := create(), create()
	inspected, err := InspectWalletForMigration(a.Email, "10.000000", "", a.Id)
	require.NoError(t, err)
	require.Equal(t, a.Id, inspected.Id)
	_, err = InspectWalletForMigration(a.Username, "10", "", b.Id)
	require.ErrorContains(t, err, "user changed")
	_, err = InspectWalletForMigration(a.Username, "10.000001", "", a.Id)
	require.ErrorIs(t, err, ErrWalletMigrationVerification)
	var untouched User
	require.NoError(t, DB.First(&untouched, a.Id).Error)
	require.Equal(t, 5_000_000, untouched.Quota)

	id := uuid.NewString()
	_, err = TransferWalletForMigration(context.Background(), id, b.Id, a.Username, "10")
	require.ErrorContains(t, err, "user changed")
	first, err := TransferWalletForMigration(context.Background(), id, a.Id, a.Username, "10")
	require.NoError(t, err)
	// Receipt replay is owner-checked before balance verification: the wallet is now zero.
	repeat, err := TransferWalletForMigration(context.Background(), id, a.Id, a.Email, "999")
	require.NoError(t, err)
	recovered, err := InspectWalletForMigration(a.Email, "999", id, a.Id)
	require.NoError(t, err)
	require.Equal(t, a.Id, recovered.Id)
	require.Equal(t, first.TransferID, repeat.TransferID)
	_, err = TransferWalletForMigration(context.Background(), id, b.Id, b.Username, "10")
	require.ErrorContains(t, err, "another user")

	WalletMigrationEnabled = false
	_, err = TransferWalletForMigration(context.Background(), uuid.NewString(), b.Id, b.Username, "10")
	require.ErrorContains(t, err, "wallet_migration_not_enabled")
	repeat, err = TransferWalletForMigration(context.Background(), id, a.Id, a.Username, "0")
	require.NoError(t, err)
	require.Equal(t, first.TransferID, repeat.TransferID)
	WalletMigrationEnabled = true

	require.NoError(t, DB.Model(&User{}).Where("id = ?", a.Id).Update("quota", 2_500_000).Error)
	_, err = TransferWalletForMigration(context.Background(), uuid.NewString(), a.Id, a.Username, "4.999999")
	require.ErrorIs(t, err, ErrWalletMigrationVerification)
	require.NoError(t, DB.First(&untouched, a.Id).Error)
	require.Equal(t, 2_500_000, untouched.Quota)
	next, err := TransferWalletForMigration(context.Background(), uuid.NewString(), a.Id, a.Username, "5")
	require.NoError(t, err)
	require.Equal(t, 2_500_000, next.Amount)
	require.NotEqual(t, first.TransferID, next.TransferID)
	zero, err := TransferWalletForMigration(context.Background(), uuid.NewString(), a.Id, a.Username, "0")
	require.NoError(t, err)
	require.Zero(t, zero.Amount)
}
