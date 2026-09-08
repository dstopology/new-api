package model

import (
	"errors"
	"os"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Enable on every serving node before offering online migration. Never change
// this process-wide mode while work is running. Old nodes cannot join the proof.
var WalletMigrationEnabled = os.Getenv("WALLET_MIGRATION_ENABLED") == "true"

// WalletMigrationAccount is the distributed admission/drain owner. UserID zero
// represents task polling iterations, acquired BEFORE their task snapshot.
// Activity is deliberately durable and has no TTL: a dead writer is not drained.
type WalletMigrationAccount struct {
	UserID      int    `gorm:"primaryKey;autoIncrement:false"`
	FrozenID    string `gorm:"size:128;not null;default:''"`
	DirectQuota bool   `gorm:"not null;default:false"`
	Activity    int64  `gorm:"not null;default:0"`
}

var ErrWalletMigrationBusy = errors.New("wallet_migration_busy")

// If retaining a financial write itself fails, preserve its already admitted
// request/refund/poller activity. No later completion may erase that evidence.
// Normal SQL failures after retention already have their own durable activity.
var walletRetentionFailures sync.Map

func lockWalletAccount(tx *gorm.DB, userID int) (*WalletMigrationAccount, error) {
	account := WalletMigrationAccount{UserID: userID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return nil, err
	}
	if err := withRowLock(tx).First(&account, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

// BeginWalletActivity admits requests or retains previously admitted work.
// allowFrozen is only for existing refunds and the task poller (userID zero).
func BeginWalletActivity(userID int, allowFrozen bool) error {
	if !WalletMigrationEnabled {
		return nil
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockWalletAccount(tx, userID)
		if err != nil {
			return err
		}
		if account.FrozenID != "" && !allowFrozen {
			return ErrWalletMigrationBusy
		}
		return tx.Model(account).Where("user_id = ?", userID).Update("activity", gorm.Expr("activity + 1")).Error
	})
}

func EndWalletActivity(userID int, count int64) {
	if !WalletMigrationEnabled {
		return
	}
	if _, failed := walletRetentionFailures.Load(userID); failed {
		return
	}
	if userID == 0 {
		failed := false
		walletRetentionFailures.Range(func(_, _ any) bool { failed = true; return false })
		if failed {
			return
		}
	}
	result := DB.Model(&WalletMigrationAccount{}).
		Where("user_id = ? AND activity >= ?", userID, count).
		Update("activity", gorm.Expr("activity - ?", count))
	if result.Error != nil || result.RowsAffected != 1 {
		common.SysError("wallet migration activity completion failed; reconciliation required")
	}
}

// Retain before scheduling so request completion cannot overtake a queued refund.
// If retention fails, finish synchronously under the caller's existing activity.
func GoWalletRefund(userID int, refund func()) {
	if err := BeginWalletActivity(userID, true); err != nil {
		refund()
		return
	}
	gopool.Go(func() {
		refund()
		EndWalletActivity(userID, 1)
	})
}

// The snapshot and its settlement share one lifetime, including worker joins.
// No polling is paused; migration waits for an interval with no active snapshot.
func RunWalletTaskPoll(poll func()) {
	if err := BeginWalletActivity(0, true); err != nil {
		common.SysError("wallet migration cannot track task polling: " + err.Error())
		return
	}
	poll()
	EndWalletActivity(0, 1)
}

func walletUsesDatabase(userID int) (bool, error) {
	if !WalletMigrationEnabled {
		return false, nil
	}
	var database bool
	err := DB.Model(&WalletMigrationAccount{}).Where("user_id = ?", userID).
		Select("direct_quota").Scan(&database).Error
	return database, err
}

// The same retained activity owns a direct SQL write or is handed to its batch.
// Failed/ambiguous writes leave activity outstanding; callers cannot erase it by
// logging the error and returning successfully from their request or poller.
func beginWalletQuotaWrite(userID int, allowBatch bool) (batch, direct bool, err error) {
	if !WalletMigrationEnabled {
		return allowBatch, false, nil
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		account, err := lockWalletAccount(tx, userID)
		if err != nil {
			return err
		}
		direct = account.DirectQuota
		batch = allowBatch && !direct
		return tx.Model(account).Where("user_id = ?", userID).Update("activity", gorm.Expr("activity + 1")).Error
	})
	if err != nil {
		walletRetentionFailures.Store(userID, struct{}{})
	}
	return batch, direct, err
}
