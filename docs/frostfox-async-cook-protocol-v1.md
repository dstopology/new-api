# FrostFox 异步 Cook 接入协议 v1

本文档定义 FrostFox/FrostFoxNoder 接入 new-api 异步图片任务的稳定边界。协议目标是让 Graph 中的远程生成节点具备可恢复、可轮询、可取货、可持久化的正式异步 Cook 能力。

本文中的 `MUST` 表示必须实现，`SHOULD` 表示建议实现。时间字段均为 Unix 秒；所有平台 ID 建议使用 UUIDv7 或 ULID。

## 1. 系统边界

```text
Browser
  -> FrostFox API
  -> FrostFox Job DB
  -> FrostFox Worker
  -> new-api
  -> upstream provider

new-api completed
  -> FrostFox Worker 鉴权下载
  -> FrostFox 持久存储
  -> FrostFox Asset
  -> Graph 下游节点继续 Cook
```

浏览器只与 FrostFox 通信。new-api Token 只能由 FrostFox 后端保存和使用，不能返回给浏览器，也不能写入日志、任务请求快照或错误消息。

FrostFox 的 `ready` 必须表示所有结果已经保存到 FrostFox 自己的持久存储，并创建了平台 Asset。new-api 返回的临时 URL 不是最终 Asset。

## 2. FrostFox 对浏览器的接口

以下路径是建议的 FrostFox 平台协议，可以按现有路由命名调整，但字段语义应保持一致。

### 2.1 创建异步 Cook

```http
POST /api/v1/cook-jobs
Authorization: Bearer <frostfox-session>
Idempotency-Key: <uuid>
Content-Type: application/json
```

```json
{
  "operator": "image.generate",
  "graph_id": "graph_01J...",
  "node_id": "node_01J...",
  "cook_revision": 17,
  "input": {
    "model": "nano-banana-pro-1k",
    "prompt": "cinematic city at night",
    "n": 1,
    "aspect_ratio": "16:9",
    "image_size": "1K"
  }
}
```

成功必须返回 `202 Accepted`：

```json
{
  "id": "job_01J...",
  "operator": "image.generate",
  "status": "created",
  "progress": 0,
  "created_at": 1784317600,
  "updated_at": 1784317600
}
```

FrostFox 必须先创建 Job 并提交数据库事务，再向浏览器返回。`Idempotency-Key` 在同一用户下必须唯一：

- 相同 Key、相同请求哈希：返回已有 Job，不创建新任务。
- 相同 Key、不同请求哈希：返回 `409 Conflict`。
- 浏览器刷新或自身超时后，只能使用相同 Key 重试 FrostFox 接口。

### 2.2 查询异步 Cook

```http
GET /api/v1/cook-jobs/{job_id}
Authorization: Bearer <frostfox-session>
```

处理中：

```json
{
  "id": "job_01J...",
  "operator": "image.generate",
  "status": "generating",
  "progress": 60,
  "created_at": 1784317600,
  "updated_at": 1784317620
}
```

完成：

```json
{
  "id": "job_01J...",
  "operator": "image.generate",
  "status": "ready",
  "progress": 100,
  "assets": [
    {
      "id": "asset_01J...",
      "kind": "image",
      "content_type": "image/png",
      "width": 1024,
      "height": 1024,
      "size": 1250341,
      "sha256": "<hex>",
      "url": "https://frostfox.example/assets/asset_01J..."
    }
  ],
  "created_at": 1784317600,
  "updated_at": 1784317630,
  "completed_at": 1784317630
}
```

失败：

```json
{
  "id": "job_01J...",
  "operator": "image.generate",
  "status": "failed",
  "progress": 60,
  "error": {
    "code": "provider_failed",
    "message": "Image generation failed",
    "retryable": false
  },
  "created_at": 1784317600,
  "updated_at": 1784317625,
  "completed_at": 1784317625
}
```

浏览器可以每 2 至 3 秒查询 FrostFox。浏览器查询频率不应直接决定 FrostFox 对 new-api 的查询频率。

## 3. FrostFox Worker 调用 new-api

设：

```text
NEW_API_BASE_URL=https://<new-api-host>
NEW_API_TOKEN=<用户保存的 Key>
```

每个 FrostFox Job 在创建时生成一个永久不变的 `provider_idempotency_key`。该值必须先入库，再提交 new-api。

new-api 会在调用上游前按“用户 + `Idempotency-Key`”创建唯一 Task reservation，并在上游任务 ID 和公开响应持久化后才返回 HTTP `200`。相同 Key 与相同语义请求会回放已有任务，不会再次请求上游或重复计费；相同 Key 与不同请求返回 HTTP `409 idempotency_conflict`。

因此 FrostFox 可以在连接断开、超时或 `5xx` 后使用完全相同的 `provider_idempotency_key` 和请求体重试 POST。禁止为重试生成新 Key。

### 3.1 文生图提交

```http
POST {NEW_API_BASE_URL}/v1/images/generations
Authorization: Bearer {NEW_API_TOKEN}
Idempotency-Key: {provider_idempotency_key}
Content-Type: application/json
Accept: application/json
```

```json
{
  "model": "nano-banana-pro-1k",
  "prompt": "cinematic city at night",
  "n": 1,
  "aspect_ratio": "16:9",
  "image_size": "1K",
  "async": true
}
```

除 `async` 必须为 `true` 外，其余生成参数由具体模型决定。FrostFox 应原样保存本次提交的规范化参数快照和请求哈希。

成功响应为 HTTP `200`：

```json
{
  "id": "task_xxx",
  "object": "image.generation",
  "model": "nano-banana-pro-1k",
  "status": "queued",
  "progress": "20%",
  "created_at": 1784317602
}
```

收到合法响应后，FrostFox 必须在同一个数据库更新中保存 `id`、任务类型和状态。`task_xxx` 是 new-api 的公开任务 ID，不是上游真实任务 ID。

### 3.2 图生图/编辑提交

```http
POST {NEW_API_BASE_URL}/v1/images/edits
Authorization: Bearer {NEW_API_TOKEN}
Idempotency-Key: {provider_idempotency_key}
Content-Type: multipart/form-data; boundary=...
Accept: application/json
```

表单至少包含：

```text
model=<model>
prompt=<prompt>
async=true
image=<binary file>
```

其他模型参数继续使用普通表单字段。提交响应与文生图相同。FrostFox 必须记录任务类型为 `image.edits`，后续不能错误地使用 generations 查询路径。

### 3.3 查询任务

文生图：

```http
GET {NEW_API_BASE_URL}/v1/images/generations/{task_id}
Authorization: Bearer {NEW_API_TOKEN}
Accept: application/json
```

图片编辑：

```http
GET {NEW_API_BASE_URL}/v1/images/edits/{task_id}
Authorization: Bearer {NEW_API_TOKEN}
Accept: application/json
```

FrostFox Worker SHOULD 每 5 秒查询一次，并加入最多正负 20% 的随机抖动。查询和结果下载不占用 new-api 的个人模型生成 RPM。

公开状态只有：

| new-api 状态 | FrostFox 状态 |
| --- | --- |
| `queued` | `queued` |
| `in_progress` | `generating` |
| `completed` | `fetching`，开始取货 |
| `failed` | `failed` |

处理中响应：

```json
{
  "id": "task_xxx",
  "object": "image.generation",
  "model": "nano-banana-pro-1k",
  "status": "in_progress",
  "progress": "60%",
  "created_at": 1784317602
}
```

失败响应：

```json
{
  "id": "task_xxx",
  "object": "image.generation",
  "model": "nano-banana-pro-1k",
  "status": "failed",
  "progress": "60%",
  "created_at": 1784317602,
  "completed_at": 1784317622,
  "error": {
    "code": "generation_failed",
    "message": "<provider message>"
  }
}
```

完成响应：

```json
{
  "id": "task_xxx",
  "object": "image.generation",
  "model": "nano-banana-pro-1k",
  "status": "completed",
  "progress": "100%",
  "created_at": 1784317602,
  "completed_at": 1784317622,
  "expires_at": 1784317922,
  "data": [
    {
      "url": "https://<new-api-host>/v1/images/generations/task_xxx/content/media_xxx",
      "b64_json": "",
      "revised_prompt": "",
      "expires_at": 1784317922
    }
  ]
}
```

new-api 从成功落盘开始保留结果 5 分钟。FrostFox 收到 `completed` 后必须立即将 Job 原子地迁移到 `fetching` 并开始下载，不能等待下一轮普通生成队列调度。

### 3.4 鉴权取货

对 `data` 中的每个元素分别请求其 `url`：

```http
GET {data[i].url}
Authorization: Bearer {NEW_API_TOKEN}
Accept: image/*
```

成功响应为 HTTP `200` 和图片二进制。FrostFox 必须：

1. 将内容流式写入临时文件或对象存储上传流，不能把大文件完整载入内存。
2. 限制最大下载大小，并验证 `Content-Type` 为允许的图片类型。
3. 计算 SHA-256；如有需要再读取图片尺寸。
4. 使用临时对象名上传，成功后原子提交为 FrostFox Asset。
5. 全部输出均已持久化后，才把 Job 标记为 `ready`。
6. 不保存 new-api Token；临时 new-api URL 在 Job 结束后可以清除。

下载 URL 仍需要同一用户的 new-api Token，不能直接交给浏览器 `<img>`。过期下载返回：

```http
HTTP/1.1 410 Gone
Content-Type: application/json
```

```json
{
  "error": {
    "code": "output_expired",
    "message": "temporary image has expired",
    "type": "new_api_error"
  }
}
```

任务过期后查询仍返回 `status: "completed"`，但会带有：

```json
{
  "output_expired": true
}
```

此时 FrostFox 必须迁移为 `expired`，不能重新提交生成请求。

## 4. FrostFox 状态机

```text
created
  -> submitting
       -> queued
       -> generating
       -> submission_unknown
       -> failed

queued <-> generating
  -> fetching
       -> ready
       -> expired
       -> failed

created/queued/generating
  -> failed
```

状态定义：

| 状态 | 含义 | 是否终态 |
| --- | --- | --- |
| `created` | FrostFox Job 已持久化，尚未由 Worker 领取 | 否 |
| `submitting` | Worker 正在执行唯一一次 new-api POST | 否 |
| `submission_unknown` | POST 结果不确定，可能已经产生费用和上游任务 | 是，当前版本需人工处理 |
| `queued` | new-api 已接收任务，等待上游执行 | 否 |
| `generating` | 上游正在生成 | 否 |
| `fetching` | new-api 已完成，FrostFox 正在持久化输出 | 否 |
| `ready` | 所有输出均已成为 FrostFox Asset | 是 |
| `failed` | 明确失败且没有可交付结果 | 是 |
| `expired` | 生成成功但 FrostFox 未在临时窗口内完成取货 | 是 |

每次状态迁移必须使用条件更新，例如只允许 `queued -> generating`，并检查受影响行数。不能依赖进程内锁维护状态。

同一 Job 同一时间只能被一个 Worker 处理。建议使用带超时的数据库租约：`lease_owner`、`lease_expires_at`。Worker 崩溃后，除 `submitting` 外的非终态可以在租约过期后由其他 Worker 接管。

对于停留在 `submitting` 且租约过期、又没有 `provider_task_id` 的 Job，可以由新 Worker 使用原 `provider_idempotency_key` 重新执行同一 POST。new-api 将返回原任务或稳定失败状态，不会创建第二个上游任务。

## 5. 重试协议

### 5.1 提交 POST

一次 FrostFox Job 可以因传输故障多次调用 new-api POST，但所有尝试必须使用完全相同的 `provider_idempotency_key` 和规范化请求。new-api 对上游最多提交一次。

| 结果 | FrostFox 行为 |
| --- | --- |
| 合法 `200` 且有 `task_xxx` | 保存任务 ID，继续查询 |
| 明确 `400/401/403/404/409/422/429` | `failed`，不自动重提 |
| 连接断开、超时、DNS/TLS 异常 | 使用相同 Key 与请求重试 POST |
| 任意 `5xx` | 使用相同 Key 与请求重试 POST |
| `2xx` 但响应无法解析或缺少任务 ID | 使用相同 Key 与请求重试 POST |
| Worker 在提交期间崩溃 | 租约恢复后使用相同 Key 与请求重试 POST |
| 回放得到 `status: failed`、`error.code: submission_unknown` | `submission_unknown`，禁止使用新 Key 自动生成 |

提交重试使用指数退避并设置平台级截止时间。相同 Key 的回放响应会带 `Idempotent-Replayed: true`；FrostFox 不需要依赖该响应头判断正确性，只需按返回的任务状态继续处理。

### 5.2 查询 GET

| 结果 | FrostFox 行为 |
| --- | --- |
| `200` | 按任务状态迁移 |
| 网络错误、`408/425/429/5xx` | 重试查询，不重新提交 |
| `401/403` | `failed`，错误码 `credential_rejected` |
| `400 task_not_exist` 或 `404` | `failed`，错误码 `provider_task_missing` |
| 查询超过平台任务截止时间 | `failed`，错误码 `provider_timeout` |

查询重试建议使用 5 秒基础间隔、正负 20% 抖动；连续异常时指数退避，最大不超过 30 秒。

### 5.3 下载 GET

| 结果 | FrostFox 行为 |
| --- | --- |
| `200` | 校验并持久化 |
| 网络错误、`408/425/429/5xx` | 在取货截止时间前重试同一个 URL |
| `401/403` | `failed`，错误码 `credential_rejected` |
| `404` | 在短间隔内重试；到截止时间仍失败则 `expired` |
| `410` 或查询出现 `output_expired: true` | `expired` |
| 类型非法、超出大小、内容损坏 | `failed`，错误码 `invalid_output` |

取货截止时间取所有输出 `expires_at` 的最小值。FrostFox SHOULD 在该时间前至少预留 30 秒，不应等到最后一次重试才开始持久化。

## 6. Job 与输出持久化

FrostFox Job 至少保存以下字段：

```text
id
user_id
operator
graph_id
node_id
cook_revision
status
progress
request_json
request_hash
client_idempotency_key
provider_idempotency_key
provider_kind              // new_api
provider_action            // image.generations | image.edits
provider_task_id           // task_xxx，内部字段
provider_created_at
output_expires_at
next_poll_at
submit_attempted_at
poll_attempts
fetch_attempts
lease_owner
lease_expires_at
error_code
error_message
created_at
updated_at
completed_at
```

建议约束：

```text
UNIQUE(user_id, client_idempotency_key)
UNIQUE(provider_kind, provider_task_id)
```

每个输出单独保存：

```text
id
job_id
position
status                    // pending | downloading | stored | failed
source_url                // 取货完成后可清除
source_expires_at
asset_id
content_type
size
sha256
revised_prompt
created_at
updated_at
```

并设置：

```text
UNIQUE(job_id, position)
```

多图任务必须允许逐个输出重试，但只有所有位置均为 `stored` 时 Job 才能进入 `ready`。部分下载成功、部分过期时，Job 仍为 `expired`；未交付的临时对象应由 FrostFox 清理任务回收。

## 7. Graph/Cook 集成约束

异步算子可以使用以下平台内部接口：

```ts
interface AsyncOperator<Input> {
  submit(input: Input, context: CookContext): Promise<{ jobId: string }>;
  getStatus(jobId: string): Promise<JobStatus>;
  getResult(jobId: string): Promise<Asset[]>;
}
```

`getResult()` 只能读取 FrostFox 已持久化的 Asset，不能在 Graph 请求线程中临时从 new-api 下载。

调度器必须遵守：

- 异步节点提交后释放当前执行线程，其下游节点进入等待状态。
- Job 进入 `ready` 时，通过数据库事件、消息队列或可靠 Outbox 唤醒 Graph。
- 唤醒操作必须幂等；同一 Job 重复发送完成事件不能导致下游重复 Cook。
- 页面刷新只恢复对 FrostFox Job 的观察，不触发新的 provider 提交。
- 第一版可以不做节点结果缓存。以后按节点指纹复用时，缓存范围必须至少包含 `user_id`、算子版本、模型、完整参数和所有输入 Asset 哈希。

## 8. 错误码建议

FrostFox 对浏览器只暴露稳定的平台错误码，不原样透传可能包含敏感信息的上游错误：

| 错误码 | 含义 | 可由用户重新创建任务 |
| --- | --- | --- |
| `invalid_request` | 参数不合法 | 是，修正参数后 |
| `credential_rejected` | 用户保存的 new-api Key 无效 | 是，更新凭据后 |
| `quota_exhausted` | 余额或配额不足 | 是，处理配额后 |
| `rate_limited` | 提交被限流 | 是，稍后由用户主动重试 |
| `provider_failed` | 上游明确生成失败 | 是，由用户决定 |
| `provider_task_missing` | 已保存任务在 new-api 不存在 | 否，需排查 |
| `provider_timeout` | 任务超过 FrostFox 截止时间 | 由用户决定 |
| `submission_unknown` | 不确定是否已经提交和计费 | 否，禁止自动重提 |
| `output_expired` | 未在 5 分钟窗口内取货 | 否，禁止自动重提 |
| `invalid_output` | 返回内容类型、大小或文件校验失败 | 否，需排查 |

日志中可以记录 FrostFox `job_id`、new-api `task_id`、HTTP 状态码、耗时、重试次数和错误码；不得记录 Authorization、用户 Key、原始图片二进制或完整敏感 Prompt。

## 9. 第一阶段验收标准

FrostFoxNoder 第一阶段完成以下用例即可开始联调：

1. 同一浏览器 `Idempotency-Key` 重放不会创建第二个 FrostFox Job。
2. 同一生成 Job 的全部 new-api POST 都使用同一 Key，且只产生一个公开任务和一次上游提交。
3. 收到 `task_xxx` 后，进程重启仍能继续查询。
4. 查询网络错误不会触发第二次 POST。
5. `completed` 后能使用同一用户 Token 下载并生成 FrostFox Asset。
6. 多图任务只有全部输出保存成功才进入 `ready`。
7. new-api 下载返回 `410` 时 Job 进入 `expired`。
8. Worker 在 `queued`、`generating`、`fetching` 阶段崩溃后可由租约恢复。
9. Worker 在 `submitting` 阶段崩溃后能以同一 Key 恢复；网关明确返回 `submission_unknown` 时停止自动生成。
10. 浏览器刷新后能通过 FrostFox Job ID 恢复进度与最终 Asset。

## 10. 当前部署约束

- new-api 临时图片默认保留 300 秒。
- new-api 查询和下载需要创建任务时同一用户的 Bearer Token。
- new-api 查询和下载不占用个人模型生成 RPM；提交仍正常计费和限流。
- new-api 当前应保持单实例运行，或让多个实例共享 `ASYNC_MEDIA_DIR`。
- new-api 已实现本地 `Idempotency-Key` 去重与先落库后返回；FrostFox 正式计费请求必须始终发送该 Header。
