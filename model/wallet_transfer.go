package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// WalletTransfer is the durable idempotency record for a one-time external migration.
type WalletTransfer struct {
	TransferID   string    `json:"transfer_id" gorm:"primaryKey;size:36"`
	MigrationID  string    `json:"migration_id" gorm:"size:128;not null;uniqueIndex"`
	UserID       int       `json:"user_id" gorm:"not null;index"`
	Amount       int       `json:"amount" gorm:"not null"`
	QuotaPerUnit float64   `json:"quota_per_unit" gorm:"not null"`
	CreatedAt    time.Time `json:"created_at"`
}

var ErrWalletMigrationVerification = errors.New("wallet_migration_verification_failed")

// InspectWalletForMigration checks identity and a balance estimate within USD 1 without changing wallet state.
// A pending operation may recover an owner-checked receipt after the exported wallet has already been zeroed.
func InspectWalletForMigration(username, expectedBalance, migrationID string, expectedUserID int) (*User, error) {
	user, err := resolveWalletMigrationUser(DB, username)
	if err != nil {
		return nil, err
	}
	if expectedUserID <= 0 || len(migrationID) > 128 {
		return nil, ErrWalletMigrationVerification
	}
	if expectedUserID != 0 && user.Id != expectedUserID {
		return nil, errors.New("migration user changed")
	}
	if migrationID != "" {
		if _, err := lookupWalletTransfer(DB, migrationID, user.Id); err == nil {
			return user, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if !walletMigrationBalanceMatches(user.Quota, expectedBalance) {
		return nil, ErrWalletMigrationVerification
	}
	return user, nil
}

func resolveWalletMigrationUser(db *gorm.DB, username string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 128 {
		return nil, ErrWalletMigrationVerification
	}
	var user User
	if err := db.Where("username = ? OR email = ?", username, username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWalletMigrationVerification
		}
		return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if user.Status != common.UserStatusEnabled || user.Role >= common.RoleAdminUser {
		return nil, ErrWalletMigrationVerification
	}
	return &user, nil
}

func walletMigrationBalanceMatches(quota int, raw string) bool {
	value := strings.TrimSpace(raw)
	if quota < 0 || value == "" || len(value) > 64 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) || common.QuotaPerUnit <= 0 {
		return false
	}
	dotSeen, digitSeen := false, false
	for _, char := range value {
		switch {
		case char >= '0' && char <= '9':
			digitSeen = true
		case char == '.' && !dotSeen:
			dotSeen = true
		default:
			return false
		}
	}
	if !digitSeen {
		return false
	}
	amount, err := decimal.NewFromString(value)
	if err != nil || amount.IsNegative() {
		return false
	}
	unit, err := decimal.NewFromString(strconv.FormatFloat(common.QuotaPerUnit, 'f', -1, 64))
	return err == nil && amount.Mul(unit).Sub(decimal.NewFromInt(int64(quota))).Abs().LessThanOrEqual(unit)
}

func lookupWalletTransfer(tx *gorm.DB, migrationID string, userID int) (*WalletTransfer, error) {
	var receipt WalletTransfer
	if err := tx.Where("migration_id = ?", migrationID).First(&receipt).Error; err != nil {
		return nil, err
	}
	if receipt.UserID != userID {
		return nil, errors.New("migration_id belongs to another user")
	}
	return &receipt, nil
}

// TransferWalletForMigration exports the committed wallet snapshot without draining work.
// Later settlement stays on the old wallet; replay always returns the original receipt.
func TransferWalletForMigration(ctx context.Context, migrationID string, expectedUserID int, username, expectedBalance string) (*WalletTransfer, error) {
	if migrationID == "" || len(migrationID) > 128 || expectedUserID <= 0 {
		return nil, errors.New("invalid migration identity")
	}
	db := DB.WithContext(ctx)
	user, err := resolveWalletMigrationUser(db, username)
	if err != nil {
		return nil, err
	}
	if user.Id != expectedUserID {
		return nil, errors.New("migration user changed")
	}
	// A committed debit is recoverable even though the wallet balance is now zero.
	if receipt, err := lookupWalletTransfer(db, migrationID, user.Id); !errors.Is(err, gorm.ErrRecordNotFound) {
		return receipt, err
	}
	if !WalletMigrationEnabled {
		return nil, errors.New("wallet_migration_not_enabled")
	}
	var receipt *WalletTransfer
	err = db.Transaction(func(tx *gorm.DB) error {
		account, err := lockWalletAccount(tx, user.Id)
		if err != nil {
			return err
		}
		// Another caller can commit this ID after the optimistic lookup above.
		if existing, err := lookupWalletTransfer(tx, migrationID, user.Id); !errors.Is(err, gorm.ErrRecordNotFound) {
			receipt = existing
			return err
		}
		if account.FrozenID != "" && account.FrozenID != migrationID {
			return ErrWalletMigrationBusy
		}
		var current User
		if err := withRowLock(tx).First(&current, user.Id).Error; err != nil {
			return err
		}
		if current.Status != common.UserStatusEnabled ||
			current.Role >= common.RoleAdminUser ||
			!walletMigrationBalanceMatches(current.Quota, expectedBalance) {
			return ErrWalletMigrationVerification
		}
		receipt = &WalletTransfer{TransferID: uuid.NewString(), MigrationID: migrationID, UserID: user.Id, Amount: current.Quota, QuotaPerUnit: common.QuotaPerUnit}
		if err := tx.Model(&current).Update("quota", 0).Error; err != nil {
			return err
		}
		if err := tx.Create(receipt).Error; err != nil {
			return err
		}
		// Never erase activity. Existing batches/refunds still own their late deltas.
		return tx.Model(account).Updates(map[string]any{"direct_quota": true, "frozen_id": ""}).Error
	})
	if err != nil {
		// Recover only within this attempt's budget; an expired attempt can replay its ID later.
		if existing, lookupErr := lookupWalletTransfer(db, migrationID, user.Id); lookupErr == nil {
			return existing, nil
		}
		return nil, err
	}
	return receipt, nil
}

// Cancellation never touches money or activity, and cannot cancel another ID.
func CancelWalletMigration(ctx context.Context, userID int, migrationID string) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		account, err := lockWalletAccount(tx, userID)
		if err != nil {
			return err
		}
		if account.FrozenID != "" && account.FrozenID != migrationID {
			return ErrWalletMigrationBusy
		}
		return tx.Model(account).Update("frozen_id", "").Error
	})
}
