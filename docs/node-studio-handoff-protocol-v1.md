# Node 工作室账号交接协议 v1

本文档定义 new-api 与 Node 工作室之间的轻量账号交接协议。用户从 new-api 顶部导航进入 Node 工作室时，new-api 将用户名、用户面板访问令牌和可用 API Key 列表打包加密，通过浏览器表单 POST 到 Node 后端。

该协议不需要 OAuth、Redis、一次性授权码表或双方数据库互通。

## 1. new-api 配置

在系统设置的 `Operations -> Node Studio` 中配置：

| 设置键 | 说明 | 默认值 |
| --- | --- | --- |
| `node_studio.enabled` | 是否显示并启用 Node 工作室入口 | `false` |
| `node_studio.url` | Node 后端接收地址 | `https://node.dstopology.com/auth/import-keys` |
| `node_studio.secret` | 双方共享的加密文本 | 空 |

只有开关开启、接收地址有效且共享密钥非空时，顶部导航才会显示 `Node 工作室`。默认状态不会开放入口。

## 2. 浏览器交接

用户点击顶部导航后，浏览器打开：

```http
GET /api/node-studio/handoff
```

该接口只接受 new-api 已登录会话。new-api 生成加密包后返回自动提交页面，页面向配置的 Node 接收地址发送：

```http
POST /auth/import-keys
Content-Type: application/x-www-form-urlencoded

payload=v1.<nonce>.<ciphertext_and_tag>
```

`payload` 位于 POST 表单正文，不放入 URL。Node 接口完成导入后应立即返回 `302` 到不含 `payload` 的工作室地址。

## 3. 明文结构

AES-GCM 解密后的内容是 UTF-8 JSON：

```json
{
  "version": 1,
  "issued_at": 1784361600,
  "expires_at": 1784361720,
  "api_base_url": "https://api.dstopology.com",
  "user": {
    "id": 123,
    "username": "alice",
    "access_token": "dashboard-access-token"
  },
  "api_keys": [
    {
      "id": 10,
      "name": "默认 Key",
      "api_key": "sk-xxxxxxxx"
    },
    {
      "id": 11,
      "name": "图片生成",
      "api_key": "sk-yyyyyyyy"
    }
  ]
}
```

字段语义：

| 字段 | 说明 |
| --- | --- |
| `version` | 协议版本，当前固定为 `1` |
| `issued_at` | 签发时间，Unix 秒 |
| `expires_at` | 过期时间，Unix 秒；当前为签发后 120 秒 |
| `api_base_url` | Node 后端调用 new-api 时使用的基础地址 |
| `user.id` | new-api 用户 ID |
| `user.username` | new-api 用户名 |
| `user.access_token` | 用户面板访问令牌，用于查询用户余额等面板接口 |
| `api_keys` | 当前用户状态正常且未过期的 API Key 列表 |
| `api_keys[].api_key` | 可直接用于 Bearer 鉴权，固定包含 `sk-` 前缀 |

用户没有面板访问令牌时，new-api 会在第一次交接时生成一个并长期复用，不会在每次进入工作室时轮换。用户没有可用 API Key 时，`api_keys` 是空数组，交接仍然成功。

## 4. 加密格式

算法约定：

```text
secret_text = trim(NODE_STUDIO_SECRET)
key         = SHA-256(UTF-8(secret_text))
algorithm   = AES-256-GCM
nonce       = 12 个密码学随机字节
aad         = UTF-8("new-api-node-studio:v1")
tag         = 16 字节，附加在 ciphertext 末尾
encoding    = base64url，无 = 填充
```

最终文本格式：

```text
v1.<base64url(nonce)>.<base64url(ciphertext || tag)>
```

每次交接都会生成新 nonce，因此相同账号连续生成的密文也不相同。

## 5. Node.js / TypeScript 解密参考

```ts
import {
  createDecipheriv,
  createHash,
} from 'node:crypto'

type HandoffPayload = {
  version: 1
  issued_at: number
  expires_at: number
  api_base_url: string
  user: {
    id: number
    username: string
    access_token: string
  }
  api_keys: Array<{
    id: number
    name: string
    api_key: string
  }>
}

const AAD = Buffer.from('new-api-node-studio:v1', 'utf8')

export function decryptNewAPIHandoff(
  token: string,
  sharedSecret: string
): HandoffPayload {
  const [version, noncePart, sealedPart, extra] = token.split('.')
  if (version !== 'v1' || !noncePart || !sealedPart || extra) {
    throw new Error('Invalid handoff format')
  }

  const nonce = Buffer.from(noncePart, 'base64url')
  const sealed = Buffer.from(sealedPart, 'base64url')
  if (nonce.length !== 12 || sealed.length <= 16) {
    throw new Error('Invalid handoff data')
  }

  const key = createHash('sha256')
    .update(sharedSecret.trim(), 'utf8')
    .digest()
  const ciphertext = sealed.subarray(0, sealed.length - 16)
  const authTag = sealed.subarray(sealed.length - 16)
  const decipher = createDecipheriv('aes-256-gcm', key, nonce)
  decipher.setAAD(AAD)
  decipher.setAuthTag(authTag)

  const plaintext = Buffer.concat([
    decipher.update(ciphertext),
    decipher.final(),
  ])
  const payload = JSON.parse(plaintext.toString('utf8')) as HandoffPayload

  if (payload.version !== 1) throw new Error('Unsupported handoff version')
  if (payload.expires_at <= Math.floor(Date.now() / 1000)) {
    throw new Error('Expired handoff')
  }
  return payload
}
```

## 6. Node 接收端行为

Node 后端实现 `POST /auth/import-keys`：

1. 从 URL-encoded 表单读取 `payload`。
2. 使用双方相同的共享密钥解密。
3. 检查 `version` 和 `expires_at`。
4. 将 `user` 和 `api_keys` 保存到 Node 服务端会话或加密存储。
5. 创建 HttpOnly、Secure、SameSite=Lax 的 Node 登录会话 Cookie。
6. 返回 `302 /workspace`，清理浏览器当前页面中的交接内容。
7. 工作室仅向浏览器返回 Key 名称和掩码，实际请求由 Node 后端使用完整 Key 发出。

本协议没有服务端一次性 nonce 存储，同一个加密包在 120 秒内可以重复导入。Node 如需阻止短时重放，可以额外缓存密文摘要到过期时间，但不是 v1 的必需项。

## 7. 余额查询

Node 后端使用交接包中的用户面板访问令牌查询账户信息：

```http
GET <api_base_url>/api/user/self
Authorization: Bearer <user.access_token>
New-Api-User: <user.id>
```

余额位于响应的 `data.quota`。不要使用用户选择的 `api_keys[].api_key` 调用该面板接口。

生成图片、视频等模型请求则使用用户在 Node 工作室中选择的 API Key：

```http
Authorization: Bearer <api_keys[n].api_key>
```

## 8. 错误处理

| 场景 | Node 行为 |
| --- | --- |
| 缺少 `payload` | 返回 `400` |
| 格式、Base64 或 GCM 认证失败 | 返回 `400`，不得建立会话 |
| 协议版本不是 `1` | 返回 `400` |
| `expires_at` 已过期 | 返回 `400`，提示用户从 new-api 重新进入 |
| `api_keys` 为空 | 允许登录，工作室展示“暂无可用 Key” |
| 后续余额查询返回鉴权错误 | 清除 Node 中保存的账号连接并要求重新进入 |

共享密钥应只配置在 new-api 系统设置与 Node 后端环境变量中，不应进入 Node 前端代码。
