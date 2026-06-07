package model

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

const profitLogHardLimit = 200000
const ProfitCostRatioConfigOptionKey = "ProfitCostRatioConfig"
const profitCostRatioMax = 100

type ProfitQuery struct {
	StartTime       int64
	EndTime         int64
	ChannelID       int
	ChannelType     int
	ModelName       string
	Group           string
	PaymentProvider string
	PaymentMethod   string
	CostRatioConfig *ProfitCostRatioConfig
}

type ProfitSummary struct {
	StartTimestamp int64   `json:"start_timestamp"`
	EndTimestamp   int64   `json:"end_timestamp"`
	TopUpAmount    float64 `json:"topup_amount"`
	EpayWxAmount   float64 `json:"epay_wx_amount"`
	RevenueUSD     float64 `json:"revenue_usd"`
	EstimatedCost  float64 `json:"estimated_cost_usd"`
	ProfitUSD      float64 `json:"profit_usd"`
	CostRatio      float64 `json:"cost_ratio"`
	ProfitRate     float64 `json:"profit_rate"`
	RequestCount   int64   `json:"request_count"`
	FailedCount    int64   `json:"failed_count"`
	TopUpCount     int64   `json:"topup_count"`
	AvgTopUpAmount float64 `json:"avg_topup_amount"`
	Truncated      bool    `json:"truncated"`
	TruncatedLimit int     `json:"truncated_limit"`
}

type ProfitTrendItem struct {
	CreatedAt     int64   `json:"created_at"`
	TopUpAmount   float64 `json:"topup_amount"`
	RevenueUSD    float64 `json:"revenue_usd"`
	EstimatedCost float64 `json:"estimated_cost_usd"`
	ProfitUSD     float64 `json:"profit_usd"`
	RequestCount  int64   `json:"request_count"`
	FailedCount   int64   `json:"failed_count"`
}

type ProfitChannelItem struct {
	ChannelID        int     `json:"channel_id"`
	ChannelName      string  `json:"channel_name"`
	ChannelType      int     `json:"channel_type"`
	ChannelTypeName  string  `json:"channel_type_name"`
	CostRatio        float64 `json:"cost_ratio"`
	RevenueUSD       float64 `json:"revenue_usd"`
	EstimatedCost    float64 `json:"estimated_cost_usd"`
	ProfitUSD        float64 `json:"profit_usd"`
	ProfitRate       float64 `json:"profit_rate"`
	RequestCount     int64   `json:"request_count"`
	FailedCount      int64   `json:"failed_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
}

type ProfitModelItem struct {
	ModelName        string  `json:"model_name"`
	CostRatio        float64 `json:"cost_ratio"`
	RevenueUSD       float64 `json:"revenue_usd"`
	EstimatedCost    float64 `json:"estimated_cost_usd"`
	ProfitUSD        float64 `json:"profit_usd"`
	ProfitRate       float64 `json:"profit_rate"`
	RequestCount     int64   `json:"request_count"`
	FailedCount      int64   `json:"failed_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
}

type ProfitTopUpItem struct {
	PaymentProvider string  `json:"payment_provider"`
	PaymentMethod   string  `json:"payment_method"`
	Money           float64 `json:"money"`
	Count           int64   `json:"count"`
}

type ProfitOverview struct {
	Summary  ProfitSummary       `json:"summary"`
	Trends   []ProfitTrendItem   `json:"trends"`
	Channels []ProfitChannelItem `json:"channels"`
	Models   []ProfitModelItem   `json:"models"`
	TopUps   []ProfitTopUpItem   `json:"topups"`
}

type ProfitCostRatioConfig struct {
	DefaultRatio        *float64           `json:"default_ratio,omitempty"`
	ProviderRatios      map[string]float64 `json:"provider_ratios"`
	ChannelRatios       map[string]float64 `json:"channel_ratios"`
	ModelRatios         map[string]float64 `json:"model_ratios"`
	ProviderModelRatios map[string]float64 `json:"provider_model_ratios"`
	ChannelModelRatios  map[string]float64 `json:"channel_model_ratios"`
}

type profitLogRow struct {
	CreatedAt        int64
	Type             int
	ChannelID        int
	ModelName        string
	Group            string `gorm:"column:group_name"`
	Quota            int
	PromptTokens     int
	CompletionTokens int
	Other            string
}

type profitLogOther struct {
	GroupRatio     float64 `json:"group_ratio"`
	UserGroupRatio float64 `json:"user_group_ratio"`
}

type profitTopUpRow struct {
	CompleteTime    int64
	PaymentProvider string
	PaymentMethod   string
	Money           float64
}

func profitUSDFromQuota(quota float64) float64 {
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	return quota / common.QuotaPerUnit
}

func profitRate(profit float64, revenue float64) float64 {
	if revenue <= 0 {
		return 0
	}
	return math.Round(profit/revenue*10000) / 100
}

func profitRatio(cost float64, revenue float64) float64 {
	if revenue <= 0 {
		return 0
	}
	return math.Round(cost/revenue*10000) / 10000
}

func newProfitCostRatioConfig() ProfitCostRatioConfig {
	return ProfitCostRatioConfig{
		ProviderRatios:      map[string]float64{},
		ChannelRatios:       map[string]float64{},
		ModelRatios:         map[string]float64{},
		ProviderModelRatios: map[string]float64{},
		ChannelModelRatios:  map[string]float64{},
	}
}

func normalizeProfitModelKey(modelName string) string {
	return strings.ToLower(strings.TrimSpace(modelName))
}

func normalizeProfitIDKey(raw string) string {
	id, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

func normalizeProfitCompositeKey(raw string, idNormalizer func(string) string) string {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 2)
	if len(parts) != 2 {
		return ""
	}
	idKey := idNormalizer(parts[0])
	modelKey := normalizeProfitModelKey(parts[1])
	if idKey == "" || modelKey == "" {
		return ""
	}
	return idKey + "|" + modelKey
}

func isValidProfitCostRatio(ratio float64) bool {
	return !math.IsNaN(ratio) && !math.IsInf(ratio, 0) && ratio >= 0 && ratio <= profitCostRatioMax
}

func normalizeProfitCostRatioMap(raw map[string]float64, normalizeKey func(string) string) (map[string]float64, error) {
	normalized := map[string]float64{}
	for key, ratio := range raw {
		normalizedKey := normalizeKey(key)
		if normalizedKey == "" {
			continue
		}
		if !isValidProfitCostRatio(ratio) {
			return nil, fmt.Errorf("成本倍率无效：%s", key)
		}
		normalized[normalizedKey] = ratio
	}
	return normalized, nil
}

func NormalizeProfitCostRatioConfig(config ProfitCostRatioConfig) (*ProfitCostRatioConfig, error) {
	normalized := newProfitCostRatioConfig()
	if config.DefaultRatio != nil {
		if !isValidProfitCostRatio(*config.DefaultRatio) {
			return nil, errors.New("默认成本倍率无效")
		}
		defaultRatio := *config.DefaultRatio
		normalized.DefaultRatio = &defaultRatio
	}

	var err error
	normalized.ProviderRatios, err = normalizeProfitCostRatioMap(config.ProviderRatios, normalizeProfitIDKey)
	if err != nil {
		return nil, err
	}
	normalized.ChannelRatios, err = normalizeProfitCostRatioMap(config.ChannelRatios, normalizeProfitIDKey)
	if err != nil {
		return nil, err
	}
	normalized.ModelRatios, err = normalizeProfitCostRatioMap(config.ModelRatios, normalizeProfitModelKey)
	if err != nil {
		return nil, err
	}
	normalized.ProviderModelRatios, err = normalizeProfitCostRatioMap(config.ProviderModelRatios, func(key string) string {
		return normalizeProfitCompositeKey(key, normalizeProfitIDKey)
	})
	if err != nil {
		return nil, err
	}
	normalized.ChannelModelRatios, err = normalizeProfitCostRatioMap(config.ChannelModelRatios, func(key string) string {
		return normalizeProfitCompositeKey(key, normalizeProfitIDKey)
	})
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func GetProfitCostRatioConfig() (*ProfitCostRatioConfig, error) {
	common.OptionMapRWMutex.RLock()
	raw := strings.TrimSpace(common.OptionMap[ProfitCostRatioConfigOptionKey])
	common.OptionMapRWMutex.RUnlock()
	if raw == "" {
		config := newProfitCostRatioConfig()
		return &config, nil
	}

	config := newProfitCostRatioConfig()
	if err := common.UnmarshalJsonStr(raw, &config); err != nil {
		return nil, err
	}
	return NormalizeProfitCostRatioConfig(config)
}

func SaveProfitCostRatioConfig(config ProfitCostRatioConfig) (*ProfitCostRatioConfig, error) {
	normalized, err := NormalizeProfitCostRatioConfig(config)
	if err != nil {
		return nil, err
	}
	jsonBytes, err := common.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	if err := UpdateOption(ProfitCostRatioConfigOptionKey, string(jsonBytes)); err != nil {
		return nil, err
	}
	return normalized, nil
}

func lookupProfitCostRatio(config ProfitCostRatioConfig, channelID int, channelType int, modelName string) (float64, bool) {
	channelKey := strconv.Itoa(channelID)
	providerKey := strconv.Itoa(channelType)
	modelKey := normalizeProfitModelKey(modelName)

	if channelID > 0 && modelKey != "" {
		if ratio, ok := config.ChannelModelRatios[channelKey+"|"+modelKey]; ok {
			return ratio, true
		}
	}
	if channelID > 0 {
		if ratio, ok := config.ChannelRatios[channelKey]; ok {
			return ratio, true
		}
	}
	if channelType > 0 && modelKey != "" {
		if ratio, ok := config.ProviderModelRatios[providerKey+"|"+modelKey]; ok {
			return ratio, true
		}
	}
	if channelType > 0 {
		if ratio, ok := config.ProviderRatios[providerKey]; ok {
			return ratio, true
		}
	}
	if modelKey != "" {
		if ratio, ok := config.ModelRatios[modelKey]; ok {
			return ratio, true
		}
	}
	if config.DefaultRatio != nil {
		return *config.DefaultRatio, true
	}
	return 0, false
}

func channelTypeName(channelType int) string {
	if channelType == 0 {
		return "Unknown"
	}
	return constant.GetChannelTypeName(channelType)
}

func profitBucket(timestamp int64) int64 {
	return timestamp - timestamp%3600
}

func estimateCostQuota(row profitLogRow) float64 {
	if row.Quota <= 0 {
		return 0
	}
	other := profitLogOther{}
	if strings.TrimSpace(row.Other) != "" {
		_ = common.Unmarshal([]byte(row.Other), &other)
	}
	ratio := other.UserGroupRatio
	if ratio <= 0 {
		ratio = other.GroupRatio
	}
	if ratio <= 0 {
		ratio = 1
	}
	return float64(row.Quota) / ratio
}

func GetProfitOverview(query ProfitQuery) (*ProfitOverview, error) {
	costRatioConfig := query.CostRatioConfig
	if costRatioConfig == nil {
		config, err := GetProfitCostRatioConfig()
		if err != nil {
			return nil, err
		}
		costRatioConfig = config
	} else {
		normalized, err := NormalizeProfitCostRatioConfig(*costRatioConfig)
		if err != nil {
			return nil, err
		}
		costRatioConfig = normalized
	}

	channelNames := map[int]string{}
	channelTypes := map[int]int{}
	channelIDsByType := map[int][]int{}
	var channels []Channel
	if err := DB.Model(&Channel{}).Select("id, name, type").Find(&channels).Error; err != nil {
		return nil, err
	}
	for _, channel := range channels {
		channelNames[channel.Id] = channel.Name
		channelTypes[channel.Id] = channel.Type
		channelIDsByType[channel.Type] = append(channelIDsByType[channel.Type], channel.Id)
	}

	var logRows []profitLogRow
	logQuery := LOG_DB.Model(&Log{}).
		Select("created_at, type, channel_id, model_name, "+logGroupCol+" AS group_name, quota, prompt_tokens, completion_tokens, other").
		Where("created_at >= ? and created_at <= ? and (type = ? or type = ?)", query.StartTime, query.EndTime, LogTypeConsume, LogTypeError)
	if query.ChannelID > 0 {
		logQuery = logQuery.Where("channel_id = ?", query.ChannelID)
	}
	if query.ChannelType > 0 {
		channelIDs := channelIDsByType[query.ChannelType]
		if len(channelIDs) == 0 {
			logQuery = logQuery.Where("1 = 0")
		} else {
			logQuery = logQuery.Where("channel_id IN ?", channelIDs)
		}
	}
	if strings.TrimSpace(query.ModelName) != "" {
		pattern, err := containsLikePattern(strings.TrimSpace(query.ModelName))
		if err != nil {
			return nil, err
		}
		logQuery = logQuery.Where("model_name LIKE ? ESCAPE '!'", pattern)
	}
	if strings.TrimSpace(query.Group) != "" {
		logQuery = logQuery.Where(logGroupCol+" = ?", strings.TrimSpace(query.Group))
	}
	if err := logQuery.Order("created_at desc").Limit(profitLogHardLimit + 1).Find(&logRows).Error; err != nil {
		return nil, err
	}

	truncated := len(logRows) > profitLogHardLimit
	if truncated {
		logRows = logRows[:profitLogHardLimit]
	}

	var topUpRows []profitTopUpRow
	topUpQuery := DB.Model(&TopUp{}).
		Select("complete_time, payment_provider, payment_method, money").
		Where("status = ? and complete_time > 0 and complete_time >= ? and complete_time <= ?", common.TopUpStatusSuccess, query.StartTime, query.EndTime)
	if strings.TrimSpace(query.PaymentProvider) != "" {
		topUpQuery = topUpQuery.Where("payment_provider = ?", strings.TrimSpace(query.PaymentProvider))
	}
	if strings.TrimSpace(query.PaymentMethod) != "" {
		topUpQuery = topUpQuery.Where("payment_method = ?", strings.TrimSpace(query.PaymentMethod))
	}
	if err := topUpQuery.Find(&topUpRows).Error; err != nil {
		return nil, err
	}

	overview := &ProfitOverview{
		Trends:   []ProfitTrendItem{},
		Channels: []ProfitChannelItem{},
		Models:   []ProfitModelItem{},
		TopUps:   []ProfitTopUpItem{},
	}
	overview.Summary.StartTimestamp = query.StartTime
	overview.Summary.EndTimestamp = query.EndTime
	overview.Summary.Truncated = truncated
	overview.Summary.TruncatedLimit = profitLogHardLimit

	trendMap := map[int64]*ProfitTrendItem{}
	channelMap := map[int]*ProfitChannelItem{}
	modelMap := map[string]*ProfitModelItem{}
	topupMap := map[string]*ProfitTopUpItem{}

	for _, row := range logRows {
		bucket := profitBucket(row.CreatedAt)
		trend := trendMap[bucket]
		if trend == nil {
			trend = &ProfitTrendItem{CreatedAt: bucket}
			trendMap[bucket] = trend
		}

		channel := channelMap[row.ChannelID]
		if channel == nil {
			channel = &ProfitChannelItem{
				ChannelID:       row.ChannelID,
				ChannelName:     channelNames[row.ChannelID],
				ChannelType:     channelTypes[row.ChannelID],
				ChannelTypeName: channelTypeName(channelTypes[row.ChannelID]),
			}
			if channel.ChannelName == "" {
				channel.ChannelName = "Unknown"
			}
			channelMap[row.ChannelID] = channel
		}

		modelName := row.ModelName
		if modelName == "" {
			modelName = "Unknown"
		}
		modelItem := modelMap[modelName]
		if modelItem == nil {
			modelItem = &ProfitModelItem{ModelName: modelName}
			modelMap[modelName] = modelItem
		}

		if row.Type == LogTypeError {
			overview.Summary.FailedCount++
			trend.FailedCount++
			channel.FailedCount++
			modelItem.FailedCount++
			continue
		}

		revenueUSD := profitUSDFromQuota(float64(row.Quota))
		costUSD := profitUSDFromQuota(estimateCostQuota(row))
		if ratio, ok := lookupProfitCostRatio(*costRatioConfig, row.ChannelID, channelTypes[row.ChannelID], row.ModelName); ok {
			costUSD = revenueUSD * ratio
		}
		profitUSD := revenueUSD - costUSD

		overview.Summary.RequestCount++
		overview.Summary.RevenueUSD += revenueUSD
		overview.Summary.EstimatedCost += costUSD
		overview.Summary.ProfitUSD += profitUSD

		trend.RequestCount++
		trend.RevenueUSD += revenueUSD
		trend.EstimatedCost += costUSD
		trend.ProfitUSD += profitUSD

		channel.RequestCount++
		channel.RevenueUSD += revenueUSD
		channel.EstimatedCost += costUSD
		channel.ProfitUSD += profitUSD
		channel.PromptTokens += int64(row.PromptTokens)
		channel.CompletionTokens += int64(row.CompletionTokens)

		modelItem.RequestCount++
		modelItem.RevenueUSD += revenueUSD
		modelItem.EstimatedCost += costUSD
		modelItem.ProfitUSD += profitUSD
		modelItem.PromptTokens += int64(row.PromptTokens)
		modelItem.CompletionTokens += int64(row.CompletionTokens)
	}

	for _, row := range topUpRows {
		bucket := profitBucket(row.CompleteTime)
		trend := trendMap[bucket]
		if trend == nil {
			trend = &ProfitTrendItem{CreatedAt: bucket}
			trendMap[bucket] = trend
		}
		trend.TopUpAmount += row.Money
		overview.Summary.TopUpAmount += row.Money
		overview.Summary.TopUpCount++
		if row.PaymentProvider == PaymentProviderEpay && row.PaymentMethod == "wxpay" {
			overview.Summary.EpayWxAmount += row.Money
		}
		key := row.PaymentProvider + "\x00" + row.PaymentMethod
		item := topupMap[key]
		if item == nil {
			item = &ProfitTopUpItem{
				PaymentProvider: row.PaymentProvider,
				PaymentMethod:   row.PaymentMethod,
			}
			topupMap[key] = item
		}
		item.Money += row.Money
		item.Count++
	}

	if overview.Summary.TopUpCount > 0 {
		overview.Summary.AvgTopUpAmount = overview.Summary.TopUpAmount / float64(overview.Summary.TopUpCount)
	}
	overview.Summary.CostRatio = profitRatio(overview.Summary.EstimatedCost, overview.Summary.RevenueUSD)
	overview.Summary.ProfitRate = profitRate(overview.Summary.ProfitUSD, overview.Summary.RevenueUSD)

	for _, trend := range trendMap {
		overview.Trends = append(overview.Trends, *trend)
	}
	for _, channel := range channelMap {
		channel.CostRatio = profitRatio(channel.EstimatedCost, channel.RevenueUSD)
		channel.ProfitRate = profitRate(channel.ProfitUSD, channel.RevenueUSD)
		overview.Channels = append(overview.Channels, *channel)
	}
	for _, modelItem := range modelMap {
		modelItem.CostRatio = profitRatio(modelItem.EstimatedCost, modelItem.RevenueUSD)
		modelItem.ProfitRate = profitRate(modelItem.ProfitUSD, modelItem.RevenueUSD)
		overview.Models = append(overview.Models, *modelItem)
	}
	for _, item := range topupMap {
		overview.TopUps = append(overview.TopUps, *item)
	}

	return overview, nil
}
