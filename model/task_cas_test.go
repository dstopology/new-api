package model

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.UsingSQLite = true
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&Task{},
		&User{},
		&Token{},
		&Log{},
		&Channel{},
		&Ability{},
		&TopUp{},
		&Redemption{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&PerfMetric{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tasks")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM channels")
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM redemptions")
		DB.Exec("DELETE FROM subscription_orders")
		DB.Exec("DELETE FROM subscription_plans")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM perf_metrics")
	})
}

func insertTask(t *testing.T, task *Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, DB.Create(task).Error)
}

func TestInitTaskAsyncImageStoresSubmissionKey(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UserId:          9,
		UsingGroup:      "default",
		OriginModelName: "image-model",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         3,
			ApiKey:            "submission-key",
			UpstreamModelName: "upstream-image-model",
		},
	}
	task := InitTask(constant.TaskPlatformAsyncImage, info)
	require.Equal(t, "submission-key", task.PrivateData.Key)
	require.Equal(t, "task_public", task.TaskID)
}

func TestUnfinishedTaskQueriesSeparateAsyncImages(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	insertTask(t, &Task{TaskID: "task_video", Platform: constant.TaskPlatformSuno, Status: TaskStatusQueued, Progress: "20%", SubmitTime: time.Now().Unix()})
	insertTask(t, &Task{TaskID: "task_image", Platform: constant.TaskPlatformAsyncImage, Status: TaskStatusQueued, Progress: "20%", SubmitTime: time.Now().Unix()})

	general := GetAllUnFinishSyncTasks(10)
	require.Len(t, general, 1)
	require.Equal(t, "task_video", general[0].TaskID)

	images := GetAllUnfinishedTasksByPlatform(constant.TaskPlatformAsyncImage, 10)
	require.Len(t, images, 1)
	require.Equal(t, "task_image", images[0].TaskID)
}

func TestReserveAsyncImageTaskReplaysUserScopedKey(t *testing.T) {
	truncateTables(t)
	key := "job-key-1"
	first := &Task{
		TaskID:          "task_first",
		UserId:          7,
		Platform:        constant.TaskPlatformAsyncImage,
		Status:          TaskStatusNotStart,
		Progress:        "0%",
		SubmitTime:      time.Now().Unix(),
		IdempotencyKey:  &key,
		RequestHash:     "hash-one",
		SubmissionState: TaskSubmissionStateReserved,
	}
	reserved, replayed, err := ReserveAsyncImageTask(first)
	require.NoError(t, err)
	require.False(t, replayed)
	require.NotZero(t, reserved.ID)

	second := &Task{
		TaskID:          "task_second",
		UserId:          7,
		Platform:        constant.TaskPlatformAsyncImage,
		Status:          TaskStatusNotStart,
		Progress:        "0%",
		SubmitTime:      time.Now().Unix(),
		IdempotencyKey:  &key,
		RequestHash:     "hash-one",
		SubmissionState: TaskSubmissionStateReserved,
	}
	existing, replayed, err := ReserveAsyncImageTask(second)
	require.NoError(t, err)
	require.True(t, replayed)
	require.Equal(t, reserved.ID, existing.ID)
	require.Equal(t, "task_first", existing.TaskID)
}

func TestReserveAsyncImageTaskConcurrentSingleWinner(t *testing.T) {
	truncateTables(t)
	const workers = 8
	ids := make(chan int64, workers)
	replayed := make(chan bool, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := "concurrent-job-key"
			task, wasReplayed, err := ReserveAsyncImageTask(&Task{
				TaskID:          fmt.Sprintf("task_concurrent_%d", index),
				UserId:          9,
				Platform:        constant.TaskPlatformAsyncImage,
				Status:          TaskStatusNotStart,
				Progress:        "0%",
				SubmitTime:      time.Now().Unix(),
				IdempotencyKey:  &key,
				RequestHash:     "same-hash",
				SubmissionState: TaskSubmissionStateReserved,
			})
			if err != nil {
				errs <- err
				return
			}
			ids <- task.ID
			replayed <- wasReplayed
		}(i)
	}
	wg.Wait()
	close(ids)
	close(replayed)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var firstID int64
	for id := range ids {
		if firstID == 0 {
			firstID = id
		}
		require.Equal(t, firstID, id)
	}
	winners := 0
	for wasReplayed := range replayed {
		if !wasReplayed {
			winners++
		}
	}
	require.Equal(t, 1, winners)
}

func TestReserveAsyncImageTaskAllowsMultipleNilKeys(t *testing.T) {
	truncateTables(t)
	for _, taskID := range []string{"task_no_key_1", "task_no_key_2"} {
		_, replayed, err := ReserveAsyncImageTask(&Task{
			TaskID:          taskID,
			UserId:          7,
			Platform:        constant.TaskPlatformAsyncImage,
			Status:          TaskStatusNotStart,
			Progress:        "0%",
			SubmitTime:      time.Now().Unix(),
			SubmissionState: TaskSubmissionStateReserved,
		})
		require.NoError(t, err)
		require.False(t, replayed)
	}
}

func TestReservedAsyncImageTaskIsNotPolledUntilSubmitted(t *testing.T) {
	truncateTables(t)
	reserved := &Task{
		TaskID:          "task_reserved",
		UserId:          7,
		Platform:        constant.TaskPlatformAsyncImage,
		Status:          TaskStatusNotStart,
		Progress:        "0%",
		SubmitTime:      time.Now().Unix(),
		SubmissionState: TaskSubmissionStateReserved,
	}
	insertTask(t, reserved)
	insertTask(t, &Task{
		TaskID:          "task_submitted",
		UserId:          7,
		Platform:        constant.TaskPlatformAsyncImage,
		Status:          TaskStatusQueued,
		Progress:        "20%",
		SubmitTime:      time.Now().Unix(),
		SubmissionState: TaskSubmissionStateSubmitted,
	})

	tasks := GetAllUnfinishedTasksByPlatform(constant.TaskPlatformAsyncImage, 10)
	require.Len(t, tasks, 1)
	require.Equal(t, "task_submitted", tasks[0].TaskID)
}

func TestUpdateWithSubmissionStateRequiresNotStart(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:          "task_reservation_cas",
		UserId:          7,
		Platform:        constant.TaskPlatformAsyncImage,
		Status:          TaskStatusFailure,
		Progress:        "100%",
		SubmitTime:      time.Now().Unix(),
		SubmissionState: TaskSubmissionStateUnknown,
	}
	insertTask(t, task)
	task.Status = TaskStatusNotStart
	task.SubmissionState = TaskSubmissionStateSubmitted
	won, err := task.UpdateWithSubmissionState(TaskSubmissionStateUnknown)
	require.NoError(t, err)
	require.False(t, won)
}

// ---------------------------------------------------------------------------
// Snapshot / Equal — pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestSnapshotEqual_Same(t *testing.T) {
	s := taskSnapshot{
		Status:     TaskStatusInProgress,
		Progress:   "50%",
		StartTime:  1000,
		FinishTime: 0,
		FailReason: "",
		ResultURL:  "",
		Data:       json.RawMessage(`{"key":"value"}`),
	}
	assert.True(t, s.Equal(s))
}

func TestSnapshotEqual_DifferentStatus(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusSuccess, Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentProgress(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Progress: "30%", Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Progress: "60%", Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentData(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":1}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":2}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_NilVsEmpty(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: nil}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage{}}
	// bytes.Equal(nil, []byte{}) == true
	assert.True(t, a.Equal(b))
}

func TestSnapshot_Roundtrip(t *testing.T) {
	task := &Task{
		Status:     TaskStatusInProgress,
		Progress:   "42%",
		StartTime:  1234,
		FinishTime: 5678,
		FailReason: "timeout",
		PrivateData: TaskPrivateData{
			ResultURL: "https://example.com/result.mp4",
		},
		Data: json.RawMessage(`{"model":"test-model"}`),
	}
	snap := task.Snapshot()
	assert.Equal(t, task.Status, snap.Status)
	assert.Equal(t, task.Progress, snap.Progress)
	assert.Equal(t, task.StartTime, snap.StartTime)
	assert.Equal(t, task.FinishTime, snap.FinishTime)
	assert.Equal(t, task.FailReason, snap.FailReason)
	assert.Equal(t, task.PrivateData.ResultURL, snap.ResultURL)
	assert.JSONEq(t, string(task.Data), string(snap.Data))
}

// ---------------------------------------------------------------------------
// UpdateWithStatus CAS — DB integration tests
// ---------------------------------------------------------------------------

func TestUpdateWithStatus_Win(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_cas_win",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_lose",
		Status: TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	won, err := task.UpdateWithStatus(TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloaded.Status) // unchanged
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_race",
		Status: TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			t := &Task{}
			*t = Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := t.UpdateWithStatus(TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}
