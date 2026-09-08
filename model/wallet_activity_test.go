package model

import (
	"context"
	"errors"
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
	mode, batch, redis := WalletMigrationEnabled, common.BatchUpdateEnabled, common.RedisEnabled
	WalletMigrationEnabled, common.BatchUpdateEnabled, common.RedisEnabled = true, false, false
	hash, err := common.Password2Hash("wallet-test-password")
	require.NoError(t, err)
	name := "wm-" + uuid.NewString()[:16]
	user := User{Username: name, Password: hash, AffCode: name, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 5000000}
	require.NoError(t, DB.Create(&user).Error)
	t.Cleanup(func() {
		WalletMigrationEnabled, common.BatchUpdateEnabled, common.RedisEnabled = mode, batch, redis
		walletRetentionFailures.Delete(user.Id)
		DB.Where("user_id IN ?", []int{0, user.Id}).Delete(&WalletMigrationAccount{})
		DB.Where("user_id = ?", user.Id).Delete(&WalletTransfer{})
		DB.Where("user_id = ?", user.Id).Delete(&Task{})
		DB.Unscoped().Delete(&user)
	})
	return user
}

func TestWalletMigrationWaitsForAccountAndResumes(t *testing.T) {
	user := walletMigrationUser(t)
	require.NoError(t, BeginWalletActivity(user.Id, false))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type result struct {
		receipt *WalletTransfer
		err     error
	}
	completed := make(chan result, 1)
	id := uuid.NewString()
	go func() {
		receipt, err := TransferWalletForMigration(ctx, id, user.Id, user.Username, "wallet-test-password")
		completed <- result{receipt, err}
	}()
	require.Eventually(t, func() bool {
		var gate WalletMigrationAccount
		return DB.First(&gate, "user_id = ?", user.Id).Error == nil && gate.FrozenID == id
	}, 3*time.Second, 10*time.Millisecond)
	require.ErrorIs(t, BeginWalletActivity(user.Id, false), ErrWalletMigrationBusy)
	// Other accounts are still admitted while the target finishes its debit.
	require.NoError(t, BeginWalletActivity(user.Id+1000000, false))
	EndWalletActivity(user.Id+1000000, 1)
	t.Cleanup(func() { DB.Where("user_id = ?", user.Id+1000000).Delete(&WalletMigrationAccount{}) })
	require.NoError(t, DecreaseUserQuota(user.Id, 500000, false))
	select {
	case <-completed:
		t.Fatal("export overtook the admitted request")
	default:
	}
	EndWalletActivity(user.Id, 1)
	got := <-completed
	require.NoError(t, got.err)
	require.Equal(t, 4500000, got.receipt.Amount)
	require.NoError(t, BeginWalletActivity(user.Id, false))
	EndWalletActivity(user.Id, 1)
}

func TestWalletMigrationWaitsForDetachedBatch(t *testing.T) {
	user := walletMigrationUser(t)
	common.BatchUpdateEnabled = true
	require.NoError(t, DecreaseUserQuota(user.Id, 500000, false))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	name := "wallet-test-detached-batch"
	require.NoError(t, DB.Callback().Update().Before("gorm:begin_transaction").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			close(entered)
			<-release
		}
	}))
	t.Cleanup(func() { DB.Callback().Update().Remove(name) })
	go func() { batchUpdate(); close(done) }()
	<-entered // the local map is already empty, but its SQL has not executed
	id := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, err := TransferWalletForMigration(ctx, id, user.Id, user.Username, "wallet-test-password")
	cancel()
	require.Error(t, err)
	var count int64
	require.NoError(t, DB.Model(&WalletTransfer{}).Where("migration_id = ?", id).Count(&count).Error)
	require.Zero(t, count)
	close(release)
	<-done
	require.NoError(t, DB.Callback().Update().Remove(name))
	receipt, err := TransferWalletForMigration(context.Background(), id, user.Id, user.Username, "wallet-test-password")
	require.NoError(t, err)
	require.Equal(t, 4500000, receipt.Amount)
	// Enrollment is permanent: later credits/debits bypass batching and Redis.
	common.RedisEnabled = true // no Redis client: the DB path must not touch it
	require.NoError(t, IncreaseUserQuota(user.Id, 700000, false))
	quota, err := GetUserQuota(user.Id, false)
	require.NoError(t, err)
	require.Equal(t, 700000, quota)
	base, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, quota, base.Quota)
}

func TestWalletMigrationRetainsDeferredRefundAndPoller(t *testing.T) {
	user := walletMigrationUser(t)
	release, done := make(chan struct{}), make(chan struct{})
	GoWalletRefund(user.Id, func() { <-release; close(done) })
	require.NoError(t, BeginWalletActivity(0, true))
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	_, err := TransferWalletForMigration(ctx, uuid.NewString(), user.Id, user.Username, "wallet-test-password")
	cancel()
	require.Error(t, err)
	close(release)
	<-done
	EndWalletActivity(0, 1)
	require.Eventually(t, func() bool {
		var gate WalletMigrationAccount
		return DB.First(&gate, "user_id = ?", user.Id).Error == nil && gate.Activity == 0
	}, time.Second, 10*time.Millisecond)
	receipt, err := TransferWalletForMigration(context.Background(), uuid.NewString(), user.Id, user.Username, "wallet-test-password")
	require.NoError(t, err)
	require.Equal(t, 5000000, receipt.Amount)
}

func TestWalletMigrationFailedMoneyWriteCannotDrain(t *testing.T) {
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
			_, err = TransferWalletForMigration(ctx, uuid.NewString(), user.Id, user.Username, "wallet-test-password")
			require.Error(t, err)
		})
	}
}

func TestWalletMigrationPendingTaskAndStaleProfile(t *testing.T) {
	user := walletMigrationUser(t)
	task := Task{UserId: user.Id, Status: TaskStatusInProgress, Progress: "50%"}
	require.NoError(t, DB.Create(&task).Error)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	_, err := TransferWalletForMigration(ctx, uuid.NewString(), user.Id, user.Username, "wallet-test-password")
	cancel()
	require.Error(t, err)
	require.NoError(t, DB.Delete(&task).Error)
	receipt, err := TransferWalletForMigration(context.Background(), uuid.NewString(), user.Id, user.Username, "wallet-test-password")
	require.NoError(t, err)
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
