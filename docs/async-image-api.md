# 异步图片 API

异步图片复用 OpenAI 图片路径。请求中不传 `async` 或传 `false` 时，现有同步行为不变；传 `true` 时，接口立即返回任务并由后台轮询上游。

## 流式等待桥接

普通非流式请求保持原有同步 JSON 行为。对于自定义 OpenAI 兼容渠道，客户端传入 `stream=true` 时，new-api 会复用异步图片任务作为流式等待桥接：

1. 先建立本地幂等任务并向上游提交 `async=true`；提交给上游的请求会移除 `stream`，确保上游返回 JSON 任务信息。
2. 等待期间以 SSE 注释 `: PING` 保持连接，默认间隔 25 秒。
3. 后台轮询完成并把图片写入本地临时目录后，发送一条标准 OpenAI 图片 JSON `data` 事件，随后发送 `data: [DONE]`。
4. 响应头 `X-New-API-Task-ID` 提供公开任务 ID。客户端断线后可以使用同一 `Idempotency-Key` 重试，或按异步查询接口继续取货。

示例最终事件：

```text
: PING

data: {"created":1784317622,"data":[{"url":"https://api.example.com/v1/images/generations/task_xxx/content/media_xxx","expires_at":1784317922}]}

data: [DONE]
```

若自定义上游以明确的 `400`、`404`、`405`、`415` 或 `422` 拒绝异步提交，new-api 会删除本次已拒绝的本地 reservation、退款，并回退原有直连流式实现。网络错误、`429` 或不确定的 `5xx` 不会回退或重新提交，以免上游已经受理时发生重复生成和重复计费。

官方 OpenAI 地址和非 OpenAI 类型渠道默认继续使用原有直连流式实现；客户端显式同时传入 `async=true` 与 `stream=true` 时会明确选择任务桥接。

## 提交任务

```http
POST /v1/images/generations
Authorization: Bearer <token>
Idempotency-Key: <uuid>
Content-Type: application/json
```

```json
{
  "model": "nano-banana-pro-1k",
  "prompt": "电影感城市夜景",
  "aspect_ratio": "16:9",
  "image_size": "1K",
  "async": true
}
```

响应中的 ID 是网关生成的公开任务 ID，不会暴露上游任务 ID：

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

图生图可以向 `POST /v1/images/edits` 提交 JSON 或 multipart 请求，同样需要传 `async=true`。

### 提交幂等

异步图片会在调用上游之前先创建本地任务记录，并在任务记录成功保存上游任务 ID 和公开响应后才返回 HTTP `200`。

平台后端应为一次生成使用固定的 `Idempotency-Key`。Key 最长 128 个可见 ASCII 字符，并按 new-api 用户隔离：

- 相同 Key、相同语义请求会返回已有 `task_xxx`，响应头包含 `Idempotent-Replayed: true`，不会再次请求上游或重复计费。
- 相同 Key、不同请求返回 HTTP `409 idempotency_conflict`。
- JSON 字段顺序、multipart boundary 和上传文件名不参与语义差异判断；表单字段和文件内容参与请求指纹。
- 网络断开、超时或 `5xx` 后可以使用完全相同的 Key 和请求安全重试 POST。

不传 `Idempotency-Key` 时仍会先落库再请求上游，但无法通过重试关联原任务，因此正式计费接入必须传该 Header。

## 查询任务

```http
GET /v1/images/generations/{task_id}
Authorization: Bearer <token>
```

状态为 `queued`、`in_progress`、`completed` 或 `failed`。建议每 5 至 10 秒查询一次。查询和结果下载不占用用户的模型生成 RPM。

任务完成后返回本地临时图片 URL：

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
      "url": "https://api.example.com/v1/images/generations/task_xxx/content/media_xxx",
      "b64_json": "",
      "revised_prompt": "",
      "expires_at": 1784317922
    }
  ]
}
```

## 领取图片

临时图片 URL 仍需要原 API Token：

```http
GET /v1/images/generations/{task_id}/content/{media_id}
Authorization: Bearer <token>
```

平台后端应在任务完成后立即下载，并将图片保存到自己的持久存储。该 URL 不适合直接放入浏览器 `<img>`，因为浏览器图片标签不能附加 Bearer Token。

图片从任务完成并成功落盘后保留 5 分钟。过期下载返回 HTTP `410 Gone`；任务仍保持 `completed`，查询响应增加 `output_expired: true`。

## 配置

```env
ASYNC_MEDIA_DIR=./async-media
ASYNC_MEDIA_RETENTION_SECONDS=300
ASYNC_MEDIA_SWEEP_SECONDS=30
ASYNC_MEDIA_MAX_FILE_MB=64
ASYNC_MEDIA_DOWNLOAD_TIMEOUT_SECONDS=90
ASYNC_MEDIA_METADATA_RETENTION_HOURS=24
ASYNC_IMAGE_TASK_TIMEOUT_MINUTES=15
ASYNC_IMAGE_STREAM_WAIT_TIMEOUT_MINUTES=15
ASYNC_IMAGE_POLL_INTERVAL_SECONDS=5
ASYNC_IMAGE_POLL_WORKERS=8
TASK_POLLING_INTERVAL_SECONDS=15
```

Docker 镜像的工作目录是 `/data`，因此默认文件目录为 `/data/async-media`。本地文件模式适用于单实例部署；多实例必须共享该目录并保持任务轮询节点和下载节点可访问同一文件系统。

流式桥接是面向需要保持单连接等待的兼容模式。FrostFox 等可恢复任务系统仍应使用 `async=true`、任务查询和鉴权取货；这样页面刷新、进程重启或客户端总请求超时都不会丢失任务。
