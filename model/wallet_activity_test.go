package model

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func walletMigrationUser(t *testing.T) User {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&WalletTransfer{}, &WalletMigrationAccount{}, &Midjourney{}))
	mode, batch, redis, quotaPerUnit := WalletMigrationEnabled, common.BatchUpdateEnabled, common.RedisEnabled, common.QuotaPerUnit
	WalletMigrationEnabled, common.BatchUpdateEnabled, common.RedisEnabled, common.QuotaPerUnit = true, false, false, 500_000
	name := "wm-" + uuid.NewString()[:16]
	user := User{Username: name, Password: "unused", AffCode: name, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 5000000}
	require.NoError(t, DB.Create(&user).Error)
	t.Cleanup(func() {
		WalletMigrationEnabled, common.BatchUpdateEnabled, common.RedisEnabled, common.QuotaPerUnit = mode, batch, redis, quotaPerUnit
		walletRetentionFailures.Delete(user.Id)
		DB.Where("user_id IN ?", []int{0, user.Id}).Delete(&WalletMigrationAccount{})
		DB.Where("user_id = ?", user.Id).Delete(&WalletTransfer{})
		DB.Where("user_id = ?", user.Id).Delete(&Task{})
		DB.Unscoped().Delete(&user)
	})
	return user
}

func TestWalletMigrationExportsWhileAccountRemainsActive(t *testing.T) {
	user := walletMigrationUser(t)
	require.NoError(t, BeginWalletActivity(user.Id, false))
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	id := uuid.NewString()
	require.NoError(t, DecreaseUserQuota(user.Id, 500000, false))
	receipt, err := TransferWalletForMigration(ctx, id, user.Id, user.Username, "9")
	require.NoError(t, err)
	require.Equal(t, 4500000, receipt.Amount)
	var gate WalletMigrationAccount
	require.NoError(t, DB.First(&gate, "user_id = ?", user.Id).Error)
	require.EqualValues(t, 1, gate.Activity)
	require.Empty(t, gate.FrozenID)
	require.NoError(t, BeginWalletActivity(user.Id, false))
	EndWalletActivity(user.Id, 2)
}

func TestWalletMigrationDetachedBatchSettlesAfterSnapshot(t *testing.T) {
	user := walletMigrationUser(t)
	common.BatchUpdateEnabled = true
	require.NoError(t, DecreaseUserQuota(user.Id, 500000, false))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	name := "wallet-test-detached-batch"
	var paused atomic.Bool
	require.NoError(t, DB.Callback().Update().Before("gorm:begin_transaction").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && paused.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}))
	t.Cleanup(func() { DB.Callback().Update().Remove(name) })
	go func() { batchUpdate(); close(done) }()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		<-done
	})
	<-entered // the local map is already empty, but its SQL has not executed
	id := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	receipt, err := TransferWalletForMigration(ctx, id, user.Id, user.Username, "10")
	cancel()
	require.NoError(t, err)
	require.Equal(t, 5000000, receipt.Amount)
	close(release)
	<-done
	require.NoError(t, DB.Callback().Update().Remove(name))
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, -500000, quota)
	replay, err := TransferWalletForMigration(t.Context(), id, user.Id, user.Username, "0")
	require.NoError(t, err)
	require.Equal(t, receipt.TransferID, replay.TransferID)
	// Enrollment is permanent: later credits/debits bypass batching and Redis.
	common.RedisEnabled = true // no Redis client: the DB path must not touch it
	require.NoError(t, IncreaseUserQuota(user.Id, 700000, false))
	quota, err = GetUserQuota(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, 200000, quota)
	base, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, quota, base.Quota)
}

func TestWalletMigrationKeepsDeferredRefundAndPollerOwnership(t *testing.T) {
	user := walletMigrationUser(t)
	release, done := make(chan struct{}), make(chan struct{})
	refundErr := make(chan error, 1)
	GoWalletRefund(user.Id, func() {
		<-release
		refundErr <- IncreaseUserQuota(user.Id, 100000, true)
		close(done)
	})
	require.NoError(t, BeginWalletActivity(0, true))
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	id := uuid.NewString()
	receipt, err := TransferWalletForMigration(ctx, id, user.Id, user.Username, "10")
	cancel()
	require.NoError(t, err)
	require.Equal(t, 5000000, receipt.Amount)
	var poller WalletMigrationAccount
	require.NoError(t, DB.First(&poller, "user_id = ?", 0).Error)
	require.EqualValues(t, 1, poller.Activity)
	close(release)
	<-done
	require.NoError(t, <-refundErr)
	EndWalletActivity(0, 1)
	require.Eventually(t, func() bool {
		var gate WalletMigrationAccount
		return DB.First(&gate, "user_id = ?", user.Id).Error == nil && gate.Activity == 0
	}, time.Second, 10*time.Millisecond)
	replay, err := TransferWalletForMigration(t.Context(), id, user.Id, user.Username, "0")
	require.NoError(t, err)
	require.Equal(t, receipt.TransferID, replay.TransferID)
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, 100000, quota)
}

func TestWalletMigrationPreservesFailedMoneyWriteEvidence(t *testing.T) {
	for _, failRetention := range []bool{false, true} {
		t.Run(map[bool]string{false: "quota SQL", true: "retention SQL"}[failRetention], func(t *testing.T) {
			user := walletMigrationUser(t)
			require.NoError(t, BeginWalletActivity(user.Id, false))
			name := "wallet-test-sql-failure"
			require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
				if (!failRetention && tx.Statement.Table == "users") || (failRetention && tx.Statement.Table == "wallet_migration_accounts") {
					tx.AddError(errors.New("injected wallet SQL failure"))
				}
			}))
			err := DecreaseUserQuota(user.Id, 100000, true)
			require.Error(t, err)
			require.NoError(t, DB.Callback().Update().Remove(name))
			EndWalletActivity(user.Id, 1) // caller swallowed the error and returned
			var gate WalletMigrationAccount
			require.NoError(t, DB.First(&gate, "user_id = ?", user.Id).Error)
			require.Positive(t, gate.Activity)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			receipt, err := TransferWalletForMigration(ctx, uuid.NewString(), user.Id, user.Username, "10")
			require.NoError(t, err)
			require.Equal(t, 5000000, receipt.Amount)
			retained := gate.Activity
			require.NoError(t, DB.First(&gate, "user_id = ?", user.Id).Error)
			require.Equal(t, retained, gate.Activity)
		})
	}
}

func TestWalletMigrationPendingTaskAndStaleProfile(t *testing.T) {
	user := walletMigrationUser(t)
	task := Task{UserId: user.Id, Status: TaskStatusInProgress, Progress: "50%"}
	require.NoError(t, DB.Create(&task).Error)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	receipt, err := TransferWalletForMigration(ctx, uuid.NewString(), user.Id, user.Username, "10")
	cancel()
	require.NoError(t, err)
	require.NoError(t, DB.First(&task, task.ID).Error)
	require.Equal(t, "IN_PROGRESS", string(task.Status))
	user.DisplayName = "profile loaded before export"
	require.NoError(t, user.Update(false))
	require.NoError(t, inviteUser(user.Id))
	quota, err := GetUserQuota(user.Id, true)
	require.NoError(t, err)
	require.Zero(t, quota)
	require.Equal(t, 5000000, receipt.Amount)
}

func TestWalletMigrationCancelDoesNotEraseWorkOrAnotherFreeze(t *testing.T) {
	user := walletMigrationUser(t)
	require.NoError(t, BeginWalletActivity(user.Id, false))
	require.NoError(t, DB.Model(&WalletMigrationAccount{}).Where("user_id = ?", user.Id).
		Updates(map[string]any{"frozen_id": "original-operation", "direct_quota": true}).Error)
	require.ErrorIs(t, CancelWalletMigration(context.Background(), user.Id, "other-operation"), ErrWalletMigrationBusy)
	require.NoError(t, CancelWalletMigration(context.Background(), user.Id, "original-operation"))
	var gate WalletMigrationAccount
	require.NoError(t, DB.First(&gate, "user_id = ?", user.Id).Error)
	require.Empty(t, gate.FrozenID)
	require.EqualValues(t, 1, gate.Activity)
	require.True(t, gate.DirectQuota)
	EndWalletActivity(user.Id, 1)
	quota, err := GetUserQuota(user.Id, true)
	require.NoError(t, err)
	require.Equal(t, 5000000, quota)
}

func TestWalletMigrationInvitationCannotRestoreSnapshot(t *testing.T) {
	user := walletMigrationUser(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var paused atomic.Bool
	name := "wallet-test-invitation-race"
	require.NoError(t, DB.Callback().Update().Before("gorm:begin_transaction").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && paused.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}
	}))
	t.Cleanup(func() { DB.Callback().Update().Remove(name) })
	go func() { done <- inviteUser(user.Id) }()
	<-entered
	receipt, err := TransferWalletForMigration(t.Context(), uuid.NewString(), user.Id, user.Username, "10")
	close(release)
	inviteErr := <-done
	require.NoError(t, err)
	require.NoError(t, inviteErr)
	require.Equal(t, 5000000, receipt.Amount)
	var current User
	require.NoError(t, DB.First(&current, user.Id).Error)
	require.Zero(t, current.Quota)
	require.Equal(t, 1, current.AffCount)
	require.Equal(t, common.QuotaForInviter, current.AffQuota)
}
