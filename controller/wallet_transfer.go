package controller

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"time"
)

type walletTransferRequest struct {
	Action          string `json:"action"`
	MigrationID     string `json:"migration_id"`
	UserID          int    `json:"user_id"`
	Username        string `json:"username" binding:"required"`
	ExpectedBalance string `json:"expected_balance" binding:"required"`
}

func TransferWalletForMigration(c *gin.Context) {
	var req walletTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if req.UserID <= 0 || len(req.Username) > 128 || len(req.ExpectedBalance) > 64 || len(req.MigrationID) > 128 {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if req.Action == "inspect" {
		user, err := model.InspectWalletForMigration(req.Username, req.ExpectedBalance, req.MigrationID, req.UserID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"user_id": user.Id, "username": user.Username})
		return
	}
	if (req.Action != "transfer" && req.Action != "cancel") || req.MigrationID == "" || req.UserID <= 0 {
		common.ApiErrorMsg(c, "invalid request")
		return
	}
	if req.Action == "cancel" {
		user, err := model.InspectWalletForMigration(req.Username, req.ExpectedBalance, req.MigrationID, req.UserID)
		if err != nil || user.Id != req.UserID {
			common.ApiErrorMsg(c, "migration user changed")
			return
		}
		if err := model.CancelWalletMigration(c.Request.Context(), req.UserID, req.MigrationID); err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"user_id": user.Id})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	transfer, err := model.TransferWalletForMigration(ctx, req.MigrationID, req.UserID, req.Username, req.ExpectedBalance)
	if err != nil {
		if ctx.Err() != nil {
			common.ApiError(c, model.ErrWalletMigrationBusy)
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, transfer)
}
