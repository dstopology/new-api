package model

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
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

// InspectWalletForMigration authenticates without changing wallet state.
func InspectWalletForMigration(username, password string) (*User, error) {
	user := &User{Username: username, Password: password}
	if err := user.ValidateAndFill(); err != nil {
		return nil, err
	}
	if user.Role >= common.RoleAdminUser {
		return nil, errors.New("admin users cannot transfer wallet quota")
	}
	return user, nil
}

// TransferWalletForMigration freezes one account and waits outside transactions.
// The same operation can resume after a lost response or a crashed coordinator.
func TransferWalletForMigration(ctx context.Context, migrationID string, expectedUserID int, username, password string) (*WalletTransfer, error) {
	if migrationID == "" || len(migrationID) > 128 || expectedUserID <= 0 {
		return nil, errors.New("invalid migration identity")
	}
	user, err := InspectWalletForMigration(username, password)
	if err != nil {
		return nil, err
	}
	if user.Id != expectedUserID {
		return nil, errors.New("migration user changed")
	}
	lookup := func(tx *gorm.DB) (*WalletTransfer, error) {
		var receipt WalletTransfer
		err := tx.Where("migration_id = ?", migrationID).First(&receipt).Error
		if err != nil {
			return nil, err
		}
		if receipt.UserID != user.Id {
			return nil, errors.New("migration_id belongs to another user")
		}
		return &receipt, nil
	}
	if receipt, err := lookup(DB); !errors.Is(err, gorm.ErrRecordNotFound) {
		return receipt, err
	}
	if !WalletMigrationEnabled {
		return nil, errors.New("wallet_migration_not_enabled")
	}
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		account, err := lockWalletAccount(tx, user.Id)
		if err != nil {
			return err
		}
		if account.FrozenID != "" && account.FrozenID != migrationID {
			return ErrWalletMigrationBusy
		}
		return tx.Model(account).Updates(map[string]any{"frozen_id": migrationID, "direct_quota": true}).Error
	})
	if err != nil {
		return nil, err
	}
	// Bounded attempts restore admission even when streams/tasks cannot yet drain.
	// A process crash leaves a durable freeze, recoverable with retry or cancel.
	defer func() {
		resume, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := CancelWalletMigration(resume, user.Id, migrationID); err != nil {
			common.SysError("wallet migration resume failed: " + err.Error())
		}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var receipt *WalletTransfer
		err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Consistent lock order: poller gate -> target account -> wallet row.
			poller, err := lockWalletAccount(tx, 0)
			if err != nil {
				return err
			}
			if poller.Activity != 0 {
				return ErrWalletMigrationBusy
			}
			account, err := lockWalletAccount(tx, user.Id)
			if err != nil {
				return err
			}
			if existing, err := lookup(tx); !errors.Is(err, gorm.ErrRecordNotFound) {
				receipt = existing
				return err
			}
			if account.FrozenID != migrationID || account.Activity != 0 {
				return ErrWalletMigrationBusy
			}
			var pending int64
			if err := tx.Model(&Task{}).Where("user_id = ? AND (status NOT IN ? OR progress <> ?)", user.Id, []string{"SUCCESS", "FAILURE"}, "100%").Count(&pending).Error; err != nil {
				return err
			}
			if pending != 0 {
				return ErrWalletMigrationBusy
			}
			if err := tx.Model(&Midjourney{}).Where("user_id = ? AND progress <> ?", user.Id, "100%").Count(&pending).Error; err != nil {
				return err
			}
			if pending != 0 {
				return ErrWalletMigrationBusy
			}
			var current User
			if err := withRowLock(tx).First(&current, user.Id).Error; err != nil {
				return err
			}
			if current.Status != common.UserStatusEnabled || current.Role >= common.RoleAdminUser {
				return errors.New("user cannot transfer wallet quota")
			}
			if current.Quota < 0 {
				return errors.New("wallet quota is negative")
			}
			receipt = &WalletTransfer{TransferID: uuid.NewString(), MigrationID: migrationID, UserID: user.Id, Amount: current.Quota, QuotaPerUnit: common.QuotaPerUnit}
			if err := tx.Model(&current).Update("quota", 0).Error; err != nil {
				return err
			}
			if err := tx.Create(receipt).Error; err != nil {
				return err
			}
			return tx.Model(account).Update("frozen_id", "").Error
		})
		if err == nil {
			return receipt, nil
		}
		if !errors.Is(err, ErrWalletMigrationBusy) {
			// Commit acknowledgement can be lost. Only an owner-checked receipt recovers it.
			if receipt, lookupErr := lookup(DB); lookupErr == nil {
				return receipt, nil
			}
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ErrWalletMigrationBusy
		case <-ticker.C:
		}
	}
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
