package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const profitMaxRangeSeconds int64 = 31 * 24 * 60 * 60

func GetProfitSummary(c *gin.Context) {
	now := common.GetTimestamp()
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if endTimestamp <= 0 {
		endTimestamp = now
	}
	if startTimestamp <= 0 {
		startTimestamp = endTimestamp - 7*24*60*60
	}
	if startTimestamp > endTimestamp {
		startTimestamp, endTimestamp = endTimestamp, startTimestamp
	}
	if endTimestamp-startTimestamp > profitMaxRangeSeconds {
		startTimestamp = endTimestamp - profitMaxRangeSeconds
	}

	channelID, _ := strconv.Atoi(c.Query("channel_id"))
	channelType, _ := strconv.Atoi(c.Query("channel_type"))
	overview, err := model.GetProfitOverview(model.ProfitQuery{
		StartTime:       startTimestamp,
		EndTime:         endTimestamp,
		ChannelID:       channelID,
		ChannelType:     channelType,
		ModelName:       strings.TrimSpace(c.Query("model_name")),
		Group:           strings.TrimSpace(c.Query("group")),
		PaymentProvider: strings.TrimSpace(c.Query("payment_provider")),
		PaymentMethod:   strings.TrimSpace(c.Query("payment_method")),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}
