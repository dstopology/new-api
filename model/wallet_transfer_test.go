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
	_, err = InspectWalletForMigration(a.Username, "11.000001", "", a.Id)
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
	_, err = TransferWalletForMigration(context.Background(), uuid.NewString(), a.Id, a.Username, "3.999999")
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

func TestWalletMigrationApproximateBalance(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&WalletTransfer{}, &WalletMigrationAccount{}, &Task{}, &Midjourney{}))
	previousMode, previousQuotaPerUnit := WalletMigrationEnabled, common.QuotaPerUnit
	WalletMigrationEnabled = true
	t.Cleanup(func() { WalletMigrationEnabled, common.QuotaPerUnit = previousMode, previousQuotaPerUnit })
	for _, tc := range []struct {
		name    string
		quota   int
		unit    float64
		proof   string
		matches bool
	}{
		{"displayed cents", 6_172_839, 500_000, "12.35", true},
		{"lower boundary", 6_172_839, 500_000, "11.345678", true},
		{"upper boundary", 6_172_839, 500_000, "13.345678", true},
		{"below lower boundary", 6_172_839, 500_000, "11.345677", false},
		{"above upper boundary", 6_172_839, 500_000, "13.345679", false},
		{"no rounding into tolerance", 6_172_839, 500_000, "13.345678000000000001", false},
		{"zero wallet boundary", 0, 500_000, "1", true},
		{"zero wallet outside boundary", 0, 500_000, "1.000001", false},
		{"small wallet zero estimate", 250_000, 500_000, "0", true},
		{"custom quota scale", 12_345, 1_000, "13.345", true},
		{"fractional unit upper boundary", 9, 2.5, "4.6", true},
		{"fractional unit lower boundary", 9, 2.5, "2.6", true},
		{"fractional unit outside boundary", 9, 2.5, "4.6000001", false},
		{"negative estimate", 0, 500_000, "-0.5", false},
		{"exponent estimate", 5_000_000, 500_000, "1e1", false},
		{"currency symbol", 6_172_839, 500_000, "$12.35", false},
		{"group separator", 6_172_839, 500_000, "12,35", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.QuotaPerUnit = tc.unit
			name := "wm-" + uuid.NewString()[:16]
			user := User{Username: name, Password: "unused", AffCode: name, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: tc.quota}
			require.NoError(t, DB.Create(&user).Error)
			t.Cleanup(func() {
				DB.Where("user_id = ?", user.Id).Delete(&WalletTransfer{})
				DB.Where("user_id = ?", user.Id).Delete(&WalletMigrationAccount{})
				DB.Unscoped().Delete(&user)
			})
			_, inspectErr := InspectWalletForMigration(user.Username, tc.proof, "", user.Id)
			id := uuid.NewString()
			receipt, transferErr := TransferWalletForMigration(t.Context(), id, user.Id, user.Username, tc.proof)
			var current User
			require.NoError(t, DB.First(&current, user.Id).Error)
			if !tc.matches {
				require.ErrorIs(t, inspectErr, ErrWalletMigrationVerification)
				require.ErrorIs(t, transferErr, ErrWalletMigrationVerification)
				require.Nil(t, receipt)
				require.Equal(t, tc.quota, current.Quota)
				var count int64
				require.NoError(t, DB.Model(&WalletTransfer{}).Where("migration_id = ?", id).Count(&count).Error)
				require.Zero(t, count)
				require.NoError(t, DB.Model(&WalletMigrationAccount{}).Where("user_id = ? AND frozen_id <> ?", user.Id, "").Count(&count).Error)
				require.Zero(t, count)
				return
			}
			require.NoError(t, inspectErr)
			require.NoError(t, transferErr)
			require.Zero(t, current.Quota)
			require.Equal(t, tc.quota, receipt.Amount)
			require.Equal(t, tc.unit, receipt.QuotaPerUnit)
			// Replaying the original receipt must not export a later top-up.
			require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", 123).Error)
			repeat, err := TransferWalletForMigration(t.Context(), id, user.Id, user.Username, "999")
			require.NoError(t, err)
			require.Equal(t, receipt.TransferID, repeat.TransferID)
			require.Equal(t, tc.quota, repeat.Amount)
			require.NoError(t, DB.First(&current, user.Id).Error)
			require.Equal(t, 123, current.Quota)
		})
	}
}
