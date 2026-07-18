package relay

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAsyncImageSubmitContext(
	t *testing.T,
	baseURL string,
	requestHash string,
) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/images/generations",
		strings.NewReader(`{"model":"idempotency-test-model","prompt":"draw","async":true}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Idempotency-Key", "fixed-job-key")
	c.Set("platform", string(constant.TaskPlatformAsyncImage))
	common.SetContextKey(c, constant.ContextKeyUserId, 42)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyChannelId, 9)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseURL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-key")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "idempotency-test-model")
	common.SetContextKey(c, constant.ContextKeyAsyncImageIdempotencyKey, "fixed-job-key")
	common.SetContextKey(c, constant.ContextKeyAsyncImageRequestHash, requestHash)

	info, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	require.NoError(t, err)
	info.OriginModelName = "idempotency-test-model"
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c, recorder, info
}

func TestRelayTaskSubmitAsyncImagePersistsBeforeReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamSubmits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamSubmits.Add(1)
		require.Equal(t, "fixed-job-key", r.Header.Get("Idempotency-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"upstream-private-task",
			"object":"image.generation",
			"model":"idempotency-test-model",
			"status":"queued",
			"progress":"20%",
			"created_at":123
		}`))
	}))
	defer upstream.Close()

	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "relay-task.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	oldPrices := ratio_setting.ModelPrice2JSONString()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"idempotency-test-model":0}`))
	oldFreePreConsume := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	t.Cleanup(func() {
		operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = oldFreePreConsume
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
	})
	service.InitHttpClient()

	firstContext, firstRecorder, firstInfo := newAsyncImageSubmitContext(t, upstream.URL, "same-hash")
	first, taskErr := RelayTaskSubmit(firstContext, firstInfo)
	require.Nil(t, taskErr)
	require.NotNil(t, first)
	require.False(t, first.Replayed)
	require.Empty(t, firstRecorder.Body.String(), "controller must write only after RelayTaskSubmit returns a durable task")
	require.NotNil(t, first.Task)
	require.NotZero(t, first.Task.ID)
	require.Equal(t, model.TaskSubmissionStateSubmitted, first.Task.SubmissionState)
	require.Equal(t, "upstream-private-task", first.Task.PrivateData.UpstreamTaskID)

	var persisted model.Task
	require.NoError(t, db.First(&persisted, first.Task.ID).Error)
	require.Equal(t, model.TaskSubmissionStateSubmitted, persisted.SubmissionState)
	require.Equal(t, "upstream-private-task", persisted.PrivateData.UpstreamTaskID)
	require.Contains(t, string(persisted.Data), first.Task.TaskID)

	secondContext, _, secondInfo := newAsyncImageSubmitContext(t, upstream.URL, "same-hash")
	second, taskErr := RelayTaskSubmit(secondContext, secondInfo)
	require.Nil(t, taskErr)
	require.True(t, second.Replayed)
	require.Equal(t, first.Task.TaskID, second.Task.TaskID)
	require.Equal(t, int32(1), upstreamSubmits.Load())

	conflictContext, _, conflictInfo := newAsyncImageSubmitContext(t, upstream.URL, "different-hash")
	_, taskErr = RelayTaskSubmit(conflictContext, conflictInfo)
	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusConflict, taskErr.StatusCode)
	require.Equal(t, int32(1), upstreamSubmits.Load())

	var count int64
	require.NoError(t, db.Model(&model.Task{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestRelayTaskSubmitAsyncImageFailureIsStableOnReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name              string
		status            int
		submissionState   string
		replayedErrorCode string
	}{
		{
			name:              "clear rejection",
			status:            http.StatusBadRequest,
			submissionState:   model.TaskSubmissionStateRejected,
			replayedErrorCode: "submission_rejected",
		},
		{
			name:              "ambiguous server error",
			status:            http.StatusInternalServerError,
			submissionState:   model.TaskSubmissionStateUnknown,
			replayedErrorCode: "submission_unknown",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var upstreamSubmits atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				upstreamSubmits.Add(1)
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"error":{"message":"rejected"}}`))
			}))
			defer upstream.Close()

			oldDB := model.DB
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "relay-task-failure.db")), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&model.Task{}))
			model.DB = db
			t.Cleanup(func() {
				model.DB = oldDB
				sqlDB, sqlErr := db.DB()
				if sqlErr == nil {
					_ = sqlDB.Close()
				}
			})

			oldPrices := ratio_setting.ModelPrice2JSONString()
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"idempotency-test-model":0}`))
			oldFreePreConsume := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
			operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
			t.Cleanup(func() {
				operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = oldFreePreConsume
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
			})
			service.InitHttpClient()

			firstContext, _, firstInfo := newAsyncImageSubmitContext(t, upstream.URL, "failure-hash")
			first, taskErr := RelayTaskSubmit(firstContext, firstInfo)
			require.Nil(t, first)
			require.NotNil(t, taskErr)
			require.Equal(t, test.status, taskErr.StatusCode)

			persisted, exists, err := model.GetAsyncImageTaskByIdempotency(42, "fixed-job-key")
			require.NoError(t, err)
			require.True(t, exists)
			require.Equal(t, model.TaskStatus(model.TaskStatusFailure), persisted.Status)
			require.Equal(t, test.submissionState, persisted.SubmissionState)

			secondContext, _, secondInfo := newAsyncImageSubmitContext(t, upstream.URL, "failure-hash")
			second, taskErr := RelayTaskSubmit(secondContext, secondInfo)
			require.Nil(t, taskErr)
			require.True(t, second.Replayed)
			require.Contains(t, string(second.TaskData), test.replayedErrorCode)
			require.Equal(t, int32(1), upstreamSubmits.Load())
		})
	}
}
