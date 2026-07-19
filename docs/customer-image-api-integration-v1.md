# 图片生成 API 对接协议 v1

本文档面向需要将图片生成功能接入自己网站、应用或后端服务的开发者，定义鉴权、文生图、图生图、异步任务、结果下载、幂等重试和错误处理约定。

## 1. 接入信息

| 项目 | 值 |
| --- | --- |
| API Base URL | `https://api.dstopology.com` |
| 鉴权方式 | HTTP Bearer API Key |
| 请求编码 | UTF-8 |
| 时间字段 | Unix 秒时间戳 |
| 推荐调用位置 | 客户自己的后端服务 |
| 推荐生成模式 | `async=true` 异步任务模式 |

正式接入前，平台会向客户提供：

1. 用户 API Key，格式通常为 `sk-...`；
2. 已授权的图片模型 ID；
3. 对应模型支持的尺寸、比例、清晰度、图片数量和编辑能力。

这里使用的是模型 API Key，不是用户面板账户 Key。API Key 不得放入浏览器、移动端安装包、公开仓库、URL 查询参数或前端日志。

## 2. 通用请求约定

所有 API 请求均使用：

```http
Authorization: Bearer <API_KEY>
Accept: application/json
```

JSON 请求还应发送：

```http
Content-Type: application/json
```

请求示例中的变量约定：

```text
API_BASE_URL=https://api.dstopology.com
API_KEY=sk-xxxxxxxx
IMAGE_MODEL=<平台分配的图片模型 ID>
```

每个响应都可能包含 `X-Oneapi-Request-Id`。发生异常时，客户应保存该响应头以及 HTTP 状态码，便于平台定位请求。

## 3. 查询可用模型

```http
GET /v1/models
Authorization: Bearer <API_KEY>
```

```bash
curl --request GET \
  'https://api.dstopology.com/v1/models' \
  --header 'Authorization: Bearer sk-xxxxxxxx'
```

典型响应：

```json
{
  "object": "list",
  "data": [
    {
      "id": "nano-banana-pro-1k",
      "object": "model"
    }
  ]
}
```

该接口会返回当前 API Key 有权使用的全部模型，不保证每个模型都支持画图。客户端只能向图片接口提交平台已确认具备图片能力的模型 ID。模型列表和能力可能调整，不应在客户端永久写死。

## 4. 文生图

### 4.1 异步文生图，推荐

```http
POST /v1/images/generations
Authorization: Bearer <API_KEY>
Idempotency-Key: <本次业务请求的唯一值>
Content-Type: application/json
Accept: application/json
```

```json
{
  "model": "nano-banana-pro-1k",
  "prompt": "雨后东京街头，电影感摄影，霓虹倒影，细节清晰",
  "n": 1,
  "aspect_ratio": "16:9",
  "image_size": "1K",
  "async": true
}
```

```bash
curl --request POST \
  'https://api.dstopology.com/v1/images/generations' \
  --header 'Authorization: Bearer sk-xxxxxxxx' \
  --header 'Idempotency-Key: 9fe4c6b0-81ea-4ac0-9930-cad7c90fe28c' \
  --header 'Content-Type: application/json' \
  --data '{
    "model": "nano-banana-pro-1k",
    "prompt": "雨后东京街头，电影感摄影，霓虹倒影，细节清晰",
    "n": 1,
    "aspect_ratio": "16:9",
    "image_size": "1K",
    "async": true
  }'
```

成功提交返回 HTTP `200`：

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

`id` 是平台生成的公开任务 ID。客户必须保存该 ID，并通过任务查询接口获取进度。平台不会向客户暴露上游服务的真实任务 ID。

### 4.2 同步文生图

不传 `async`，或传入 `async=false`，接口使用同步模式：

```http
POST /v1/images/generations
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

```json
{
  "model": "<IMAGE_MODEL>",
  "prompt": "一只坐在窗边的黑猫，柔和自然光",
  "n": 1,
  "size": "1024x1024",
  "response_format": "url"
}
```

典型成功响应：

```json
{
  "created": 1784317622,
  "data": [
    {
      "url": "https://example.com/generated/image.png",
      "b64_json": "",
      "revised_prompt": ""
    }
  ]
}
```

如果模型支持 `response_format=b64_json`，图片内容会位于 `data[].b64_json`。并非所有模型都支持选择返回格式；客户应同时兼容 `url` 和 `b64_json` 两种结果。

同步请求可能持续较长时间。网络中断时无法可靠判断上游是否已经生成和计费，因此生产接入应优先使用异步模式，不应自动用新请求重复提交同步任务。

## 5. 图生图与图片编辑

推荐使用 `multipart/form-data`：

```http
POST /v1/images/edits
Authorization: Bearer <API_KEY>
Idempotency-Key: <本次业务请求的唯一值>
Content-Type: multipart/form-data; boundary=...
Accept: application/json
```

最小表单字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 平台分配的图片编辑模型 ID |
| `prompt` | string | 是 | 编辑要求 |
| `image` | file | 是 | 输入图片 |
| `async` | boolean | 推荐 | 正式接入建议固定为 `true` |
| `mask` | file | 否 | 蒙版，仅部分模型支持 |
| `n` | integer | 否 | 输出数量，仅在模型支持时使用 |
| `size` | string | 否 | 输出尺寸，仅在模型支持时使用 |

```bash
curl --request POST \
  'https://api.dstopology.com/v1/images/edits' \
  --header 'Authorization: Bearer sk-xxxxxxxx' \
  --header 'Idempotency-Key: 2d536cab-7f2b-4248-a122-a7fab8db7110' \
  --form 'model=<IMAGE_MODEL>' \
  --form 'prompt=保留主体，把背景改成雪山日出' \
  --form 'async=true' \
  --form 'image=@./input.png'
```

异步编辑的提交响应、任务状态和文生图一致，但后续必须使用 `/v1/images/edits/{task_id}` 查询。不要使用 generations 查询路径。

输入格式、文件数量、蒙版规则和最大尺寸由具体模型决定。通用接入建议使用 PNG、JPEG 或 WebP，并在发送前限制文件大小。服务端拒绝超大请求时会返回 HTTP `413`。

## 6. 请求字段

下表是统一图片接口的常用字段。除 `model`、`prompt` 和异步模式下的 `async` 外，其余字段是否生效由模型决定。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `model` | string | 图片模型 ID |
| `prompt` | string | 图片描述或编辑指令 |
| `n` | integer | 生成数量；数量增加通常会增加费用 |
| `size` | string | OpenAI 风格尺寸，例如 `1024x1024` |
| `quality` | string | 质量档位，例如 `standard`、`hd`、`auto` |
| `style` | string/object | 风格设置，格式由模型决定 |
| `response_format` | string | `url` 或 `b64_json`，仅部分模型支持 |
| `watermark` | boolean | 是否添加水印，仅部分模型支持 |
| `aspect_ratio` | string | 比例，例如 `1:1`、`16:9`、`9:16` |
| `image_size` | string | 模型清晰度档位，例如 `1K`、`2K` |
| `async` | boolean | `true` 返回可查询任务；省略或 `false` 为同步模式 |

不要同时发送互相冲突的尺寸字段。接入方应按照平台提供的模型参数表组装请求，不应假设所有图片模型拥有相同参数。

## 7. 幂等规则

异步生成和异步编辑必须发送 `Idempotency-Key`：

```http
Idempotency-Key: <最多 128 个可见 ASCII 字符>
```

推荐使用 UUID，或客户内部稳定且全局唯一的业务订单号。

| 场景 | 平台行为 |
| --- | --- |
| 相同 Key、相同语义请求 | 返回原有任务，不会重复调用上游或重复计费 |
| 相同 Key、不同语义请求 | HTTP `409`，错误码 `idempotency_conflict` |
| 幂等回放 | 响应头包含 `Idempotent-Replayed: true` |
| 网络断开、超时、`429` 或不确定的 `5xx` | 使用相同 Key 和完全相同的请求安全重试 |

JSON 字段顺序、multipart boundary 和上传文件名不影响请求语义；表单字段值和文件内容参与请求指纹。

一次业务生成从首次提交到最终完成必须始终使用同一个 `Idempotency-Key`。瞬时失败时禁止换新 Key 重试，否则可能产生两个任务和两次费用。

## 8. 查询异步任务

文生图：

```http
GET /v1/images/generations/{task_id}
Authorization: Bearer <API_KEY>
```

图片编辑：

```http
GET /v1/images/edits/{task_id}
Authorization: Bearer <API_KEY>
```

建议每 5 至 10 秒查询一次，并加入随机抖动。当前不提供任务完成 Webhook，接入方必须自行轮询。任务查询和结果下载不占用用户的模型生成 RPM。

状态定义：

| `status` | 是否终态 | 客户端行为 |
| --- | --- | --- |
| `queued` | 否 | 继续轮询 |
| `in_progress` | 否 | 继续轮询 |
| `completed` | 是 | 立即下载 `data[]` 中的结果 |
| `failed` | 是 | 停止轮询并记录 `error` |

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
    "message": "Image generation failed"
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
      "url": "https://api.dstopology.com/v1/images/generations/task_xxx/content/media_xxx",
      "b64_json": "",
      "revised_prompt": "",
      "expires_at": 1784317922
    }
  ]
}
```

客户端解析响应时应忽略未知字段，以便兼容协议的增量扩展。

## 9. 下载异步结果

任务完成后，直接请求响应中的 `data[i].url`：

```http
GET <data[i].url>
Authorization: Bearer <API_KEY>
Accept: image/*
```

```bash
curl --location \
  'https://api.dstopology.com/v1/images/generations/task_xxx/content/media_xxx' \
  --header 'Authorization: Bearer sk-xxxxxxxx' \
  --output result.png
```

下载 URL 不是公开 URL，仍需 Bearer API Key，因此不能直接作为浏览器 `<img src>`。客户后端应完成鉴权下载，并将文件转存到自己的对象存储或持久磁盘，再向自己的前端提供受控访问地址。

临时图片当前从成功落盘开始保留约 5 分钟，精确过期时间以 `data[].expires_at` 为准。客户应在收到 `completed` 后立即下载，不能把该 URL 当作永久资源地址。

过期后：

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

任务本身仍可能显示为 `completed`，并增加 `output_expired: true`。图片过期后平台不保证能够恢复，接入方需要重新创建生成任务。

## 10. 错误格式与处理

统一错误结构：

```json
{
  "error": {
    "message": "错误说明",
    "type": "new_api_error",
    "code": "error_code"
  }
}
```

| HTTP 状态 | 含义 | 建议处理 |
| --- | --- | --- |
| `400` | 参数、模型、任务路径或请求格式错误 | 修正请求，不自动重试 |
| `401` | API Key 缺失或无效 | 停止请求并检查 Key |
| `403` | 用户、分组、模型或 IP 无权限 | 联系平台确认授权 |
| `409` | 幂等 Key 被不同请求复用 | 修正业务幂等逻辑 |
| `410` | 临时结果已过期 | 重新创建生成任务 |
| `413` | 上传或请求体过大 | 压缩图片或降低文件大小 |
| `429` | 触发速率限制 | 延迟后使用相同幂等 Key 重试 |
| `500`、`502`、`503`、`504` | 网关或上游临时异常 | 异步提交使用相同 Key 退避重试 |

不要根据错误文案做程序分支，应优先使用 HTTP 状态码和 `error.code`。向平台反馈问题时应附带：

1. `X-Oneapi-Request-Id`；
2. 请求时间和时区；
3. HTTP 状态码与 `error.code`；
4. 模型 ID 和任务 ID；
5. 脱敏后的请求参数。

不得在工单、截图或日志中提交完整 API Key 和用户原始隐私图片。

## 11. Python 异步接入示例

```python
import os
import time
import uuid

import requests


API_BASE_URL = "https://api.dstopology.com"
API_KEY = os.environ["IMAGE_API_KEY"]
MODEL = os.environ.get("IMAGE_MODEL", "nano-banana-pro-1k")

auth_headers = {
    "Authorization": f"Bearer {API_KEY}",
    "Accept": "application/json",
}
idempotency_key = str(uuid.uuid4())

submit_response = requests.post(
    f"{API_BASE_URL}/v1/images/generations",
    headers={
        **auth_headers,
        "Content-Type": "application/json",
        "Idempotency-Key": idempotency_key,
    },
    json={
        "model": MODEL,
        "prompt": "雨后东京街头，电影感摄影，霓虹倒影，细节清晰",
        "n": 1,
        "aspect_ratio": "16:9",
        "image_size": "1K",
        "async": True,
    },
    timeout=60,
)
submit_response.raise_for_status()
task = submit_response.json()
task_id = task["id"]

deadline = time.monotonic() + 20 * 60
while True:
    if time.monotonic() >= deadline:
        raise TimeoutError(f"image task {task_id} did not finish in time")

    query_response = requests.get(
        f"{API_BASE_URL}/v1/images/generations/{task_id}",
        headers=auth_headers,
        timeout=30,
    )
    query_response.raise_for_status()
    task = query_response.json()

    if task["status"] == "failed":
        raise RuntimeError(task.get("error", {}).get("message", "image task failed"))
    if task["status"] == "completed":
        break

    time.sleep(5)

for index, image in enumerate(task.get("data", []), start=1):
    with requests.get(
        image["url"],
        headers={
            "Authorization": f"Bearer {API_KEY}",
            "Accept": "image/*",
        },
        stream=True,
        timeout=120,
    ) as download_response:
        download_response.raise_for_status()
        with open(f"result-{index}.png", "wb") as output:
            for chunk in download_response.iter_content(chunk_size=1024 * 1024):
                if chunk:
                    output.write(chunk)
```

生产代码还应加入：响应体大小限制、图片 MIME 校验、指数退避、轮询随机抖动、任务状态持久化和下载文件 SHA-256 校验。

## 12. 接入验收清单

正式开放流量前，应至少完成以下测试：

- API Key 只存储在客户后端；
- 使用分配的模型完成一次异步文生图；
- 使用 multipart 完成一次异步图片编辑；
- 页面或 Worker 重启后能根据 `task_id` 恢复轮询；
- 提交超时后使用相同 `Idempotency-Key` 重试，不产生重复任务；
- 能处理 `queued`、`in_progress`、`completed` 和 `failed`；
- 能携带 Bearer Key 下载并持久化全部 `data[]`；
- 能处理 `401`、`409`、`410`、`429` 和 `5xx`；
- 日志中不出现完整 API Key、Base64 图片或用户原图；
- 客户端不会把临时结果 URL 当作永久图片地址。

## 13. 协议版本

本协议版本为 `v1`。在 v1 内，服务端可能增加可选请求字段、响应字段或错误码；客户端必须忽略未知响应字段。删除字段、改变既有字段语义或增加破坏性必填项时，将发布新的协议版本。
