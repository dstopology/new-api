package openai

import (
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"

	"github.com/gin-gonic/gin"
)

const responsesCompletedEventType = "response.completed"

type responsesStreamCompletionFallback struct {
	sawCompleted         bool
	sawCompactionItem    bool
	hasSequenceNumber    bool
	maxSequenceNumber    int64
	response             map[string]any
	indexedOutputItems   map[int]map[string]any
	unindexedOutputItems []map[string]any
}

func newResponsesStreamCompletionFallback() *responsesStreamCompletionFallback {
	return &responsesStreamCompletionFallback{
		maxSequenceNumber:  -1,
		indexedOutputItems: make(map[int]map[string]any),
	}
}

func (f *responsesStreamCompletionFallback) Observe(data string) error {
	if f == nil || data == "" {
		return nil
	}

	var event map[string]any
	if err := common.UnmarshalJsonStr(data, &event); err != nil {
		return err
	}

	eventType := common.Interface2String(event["type"])
	if eventType == responsesCompletedEventType {
		f.sawCompleted = true
	}
	if sequenceNumber, ok := responsesStreamInteger(event["sequence_number"]); ok {
		if !f.hasSequenceNumber || sequenceNumber > f.maxSequenceNumber {
			f.maxSequenceNumber = sequenceNumber
		}
		f.hasSequenceNumber = true
	}
	if response, ok := event["response"].(map[string]any); ok {
		f.response = response
	}

	if eventType != dto.ResponsesOutputTypeItemDone {
		return nil
	}
	item, ok := event["item"].(map[string]any)
	if !ok || item == nil {
		return nil
	}

	itemType := common.Interface2String(item["type"])
	if itemType == "compaction" || itemType == "compaction_summary" {
		f.sawCompactionItem = true
	}
	if outputIndex, ok := responsesStreamInteger(event["output_index"]); ok && outputIndex >= 0 {
		f.indexedOutputItems[int(outputIndex)] = item
	} else {
		f.unindexedOutputItems = append(f.unindexedOutputItems, item)
	}
	return nil
}

func (f *responsesStreamCompletionFallback) ShouldSynthesize(info *relaycommon.RelayInfo) bool {
	if f == nil || f.sawCompleted || !f.sawCompactionItem || info == nil || info.StreamStatus == nil {
		return false
	}
	return info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors()
}

func (f *responsesStreamCompletionFallback) SendCompleted(c *gin.Context, info *relaycommon.RelayInfo) error {
	payload := f.completedEvent(info)
	jsonData, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	relayhelper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: responsesCompletedEventType}, string(jsonData))
	return nil
}

func (f *responsesStreamCompletionFallback) completedEvent(info *relaycommon.RelayInfo) map[string]any {
	now := time.Now().Unix()
	response := make(map[string]any, len(f.response)+8)
	for key, value := range f.response {
		response[key] = value
	}
	if common.Interface2String(response["id"]) == "" {
		response["id"] = "resp_" + common.GetUUID()
	}
	if common.Interface2String(response["object"]) == "" {
		response["object"] = "response"
	}
	if _, ok := response["created_at"]; !ok {
		response["created_at"] = now
	}
	response["completed_at"] = now
	response["status"] = "completed"
	response["output"] = f.outputItems()
	if common.Interface2String(response["model"]) == "" {
		response["model"] = responsesFallbackModel(info)
	}
	if _, ok := response["error"]; !ok {
		response["error"] = nil
	}
	if _, ok := response["incomplete_details"]; !ok {
		response["incomplete_details"] = nil
	}
	if _, ok := response["usage"]; !ok {
		response["usage"] = nil
	}

	sequenceNumber := int64(len(f.indexedOutputItems) + len(f.unindexedOutputItems))
	if f.hasSequenceNumber {
		sequenceNumber = f.maxSequenceNumber + 1
	}
	return map[string]any{
		"type":            responsesCompletedEventType,
		"sequence_number": sequenceNumber,
		"response":        response,
	}
}

func (f *responsesStreamCompletionFallback) outputItems() []map[string]any {
	indexes := make([]int, 0, len(f.indexedOutputItems))
	for index := range f.indexedOutputItems {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	items := make([]map[string]any, 0, len(indexes)+len(f.unindexedOutputItems))
	for _, index := range indexes {
		items = append(items, f.indexedOutputItems[index])
	}
	items = append(items, f.unindexedOutputItems...)
	return items
}

func responsesFallbackModel(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.OriginModelName != "" {
		return info.OriginModelName
	}
	if info.ChannelMeta != nil {
		return info.ChannelMeta.UpstreamModelName
	}
	return ""
}

func responsesStreamInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	case uint:
		return int64(number), true
	case uint32:
		return int64(number), true
	case uint64:
		if number <= uint64(^uint64(0)>>1) {
			return int64(number), true
		}
	case float64:
		return int64(number), true
	case string:
		parsed, err := strconv.ParseInt(number, 10, 64)
		return parsed, err == nil
	}
	return 0, false
}
