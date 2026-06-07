package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withProfitQuotaPerUnit(t *testing.T, quotaPerUnit float64) {
	t.Helper()

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = quotaPerUnit
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
	})
}

func TestGetProfitOverviewAggregatesLogsAndTopUps(t *testing.T) {
	truncateTables(t)
	withProfitQuotaPerUnit(t, 1000)

	other, err := common.Marshal(profitLogOther{GroupRatio: 2})
	require.NoError(t, err)

	require.NoError(t, DB.Create(&Channel{
		Id:     901,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-key",
		Status: common.ChannelStatusEnabled,
		Name:   "OpenAI Primary",
	}).Error)
	require.NoError(t, DB.Create(&Channel{
		Id:     902,
		Type:   constant.ChannelTypeAnthropic,
		Key:    "test-key-2",
		Status: common.ChannelStatusEnabled,
		Name:   "Anthropic Secondary",
	}).Error)
	require.NoError(t, LOG_DB.Create(&[]Log{
		{
			CreatedAt:        1700000000,
			Type:             LogTypeConsume,
			ChannelId:        901,
			ModelName:        "gpt-4o-mini",
			Group:            "vip",
			Quota:            1000,
			PromptTokens:     120,
			CompletionTokens: 80,
			Other:            string(other),
		},
		{
			CreatedAt: 1700000100,
			Type:      LogTypeError,
			ChannelId: 901,
			ModelName: "gpt-4o-mini",
			Group:     "vip",
		},
		{
			CreatedAt: 1700000200,
			Type:      LogTypeConsume,
			ChannelId: 902,
			ModelName: "claude-test",
			Group:     "default",
			Quota:     2000,
		},
	}).Error)
	require.NoError(t, DB.Create(&[]TopUp{
		{
			UserId:          1,
			Amount:          25,
			Money:           25,
			TradeNo:         "profit-wxpay",
			PaymentMethod:   "wxpay",
			PaymentProvider: PaymentProviderEpay,
			CompleteTime:    1700000300,
			Status:          common.TopUpStatusSuccess,
		},
		{
			UserId:          1,
			Amount:          10,
			Money:           10,
			TradeNo:         "profit-alipay",
			PaymentMethod:   "alipay",
			PaymentProvider: PaymentProviderEpay,
			CompleteTime:    1700000300,
			Status:          common.TopUpStatusSuccess,
		},
	}).Error)

	overview, err := GetProfitOverview(ProfitQuery{
		StartTime:       1700000000,
		EndTime:         1700003600,
		ChannelType:     constant.ChannelTypeOpenAI,
		ModelName:       "gpt-4o",
		Group:           "vip",
		PaymentProvider: PaymentProviderEpay,
		PaymentMethod:   "wxpay",
	})
	require.NoError(t, err)
	require.NotNil(t, overview)

	assert.EqualValues(t, 1700000000, overview.Summary.StartTimestamp)
	assert.EqualValues(t, 1700003600, overview.Summary.EndTimestamp)
	assert.EqualValues(t, 1, overview.Summary.RequestCount)
	assert.EqualValues(t, 1, overview.Summary.FailedCount)
	assert.InDelta(t, 1, overview.Summary.RevenueUSD, 0.0001)
	assert.InDelta(t, 0.5, overview.Summary.EstimatedCost, 0.0001)
	assert.InDelta(t, 0.5, overview.Summary.ProfitUSD, 0.0001)
	assert.InDelta(t, 50, overview.Summary.ProfitRate, 0.0001)
	assert.InDelta(t, 25, overview.Summary.TopUpAmount, 0.0001)
	assert.InDelta(t, 25, overview.Summary.EpayWxAmount, 0.0001)
	assert.EqualValues(t, 1, overview.Summary.TopUpCount)
	assert.InDelta(t, 25, overview.Summary.AvgTopUpAmount, 0.0001)

	require.Len(t, overview.Channels, 1)
	assert.Equal(t, 901, overview.Channels[0].ChannelID)
	assert.Equal(t, "OpenAI Primary", overview.Channels[0].ChannelName)
	assert.Equal(t, "OpenAI", overview.Channels[0].ChannelTypeName)
	assert.InDelta(t, 0.5, overview.Channels[0].CostRatio, 0.0001)
	assert.EqualValues(t, 120, overview.Channels[0].PromptTokens)
	assert.EqualValues(t, 80, overview.Channels[0].CompletionTokens)

	require.Len(t, overview.Models, 1)
	assert.Equal(t, "gpt-4o-mini", overview.Models[0].ModelName)
	assert.EqualValues(t, 1, overview.Models[0].RequestCount)
	assert.EqualValues(t, 1, overview.Models[0].FailedCount)

	require.Len(t, overview.TopUps, 1)
	assert.Equal(t, PaymentProviderEpay, overview.TopUps[0].PaymentProvider)
	assert.Equal(t, "wxpay", overview.TopUps[0].PaymentMethod)
	assert.InDelta(t, 25, overview.TopUps[0].Money, 0.0001)
}
