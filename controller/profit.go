package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const profitMaxRangeSeconds int64 = 31 * 24 * 60 * 60

type profitSummaryRequest struct {
	StartTimestamp  int64                       `json:"start_timestamp"`
	EndTimestamp    int64                       `json:"end_timestamp"`
	ChannelID       int                         `json:"channel_id"`
	ChannelType     int                         `json:"channel_type"`
	ModelName       string                      `json:"model_name"`
	Group           string                      `json:"group"`
	PaymentProvider string                      `json:"payment_provider"`
	PaymentMethod   string                      `json:"payment_method"`
	CostRatioConfig model.ProfitCostRatioConfig `json:"cost_ratio_config"`
}

func normalizeProfitTimeRange(startTimestamp int64, endTimestamp int64) (int64, int64) {
	now := common.GetTimestamp()
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
	return startTimestamp, endTimestamp
}

func GetProfitSummary(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	startTimestamp, endTimestamp = normalizeProfitTimeRange(startTimestamp, endTimestamp)
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

func PreviewProfitSummary(c *gin.Context) {
	var request profitSummaryRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}

	startTimestamp, endTimestamp := normalizeProfitTimeRange(request.StartTimestamp, request.EndTimestamp)
	overview, err := model.GetProfitOverview(model.ProfitQuery{
		StartTime:       startTimestamp,
		EndTime:         endTimestamp,
		ChannelID:       request.ChannelID,
		ChannelType:     request.ChannelType,
		ModelName:       strings.TrimSpace(request.ModelName),
		Group:           strings.TrimSpace(request.Group),
		PaymentProvider: strings.TrimSpace(request.PaymentProvider),
		PaymentMethod:   strings.TrimSpace(request.PaymentMethod),
		CostRatioConfig: &request.CostRatioConfig,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func GetProfitCostRatioConfig(c *gin.Context) {
	config, err := model.GetProfitCostRatioConfig()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, config)
}

func UpdateProfitCostRatioConfig(c *gin.Context) {
	var config model.ProfitCostRatioConfig
	if err := common.DecodeJson(c.Request.Body, &config); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	normalized, err := model.SaveProfitCostRatioConfig(config)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, normalized)
}
