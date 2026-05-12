# DownGo 接口文档

本文档描述 DownGo 当前 HTTP API。默认服务地址为 `http://127.0.0.1:12225`，实际端口以系统设置为准。

## 通用约定

- 请求和响应默认使用 `application/json; charset=utf-8`。
- 时间字段使用 RFC3339 字符串，例如 `2026-05-11T10:30:00Z`。
- 鉴权接口使用 Bearer Token：

```http
Authorization: Bearer <token>
```

- 对于 `EventSource`、图片和文件下载等不方便设置请求头的场景，也支持查询参数传 token：

```text
?token=<token>
```

- 错误响应通常为：

```json
{
  "error": "错误说明",
  "code": "可选错误码"
}
```

## 数据模型

### DownloadItem

```json
{
  "id": 1,
  "sourceUrl": "https://www.youtube.com/watch?v=abc",
  "normalizedUrl": "https://www.youtube.com/watch?v=abc",
  "platform": "youtube",
  "videoId": "abc",
  "title": "视频标题",
  "thumbnailUrl": "https://example.com/cover.jpg",
  "qualityLabel": "1080p",
  "container": "mp4",
  "outputFilename": "视频标题 [abc].mp4",
  "outputPath": "F:\\code2\\DownGo\\data\\downloads\\视频标题 [abc].mp4",
  "status": "completed",
  "progressPercent": 100,
  "speedBps": 0,
  "etaSeconds": 0,
  "errorMessage": "",
  "processPid": 0,
  "createdAt": "2026-05-11T10:30:00Z",
  "startedAt": "2026-05-11T10:30:05Z",
  "completedAt": "2026-05-11T10:31:00Z",
  "updatedAt": "2026-05-11T10:31:00Z"
}
```

字段说明：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | number | 下载任务 ID |
| `sourceUrl` | string | 用户提交的源地址 |
| `normalizedUrl` | string | 标准化后的地址 |
| `platform` | string | 平台：`youtube`、`bilibili` |
| `videoId` | string | 平台侧视频 ID |
| `status` | string | `resolving`、`queued`、`downloading`、`postprocessing`、`completed`、`failed`、`canceled` |
| `progressPercent` | number | 下载进度百分比 |
| `speedBps` | number | 下载速度，单位 bytes/s |
| `etaSeconds` | number | 剩余秒数 |
| `outputFilename` | string | 保存文件名 |
| `outputPath` | string | 本地保存路径 |
| `deletedAt` | string | 仅删除后内部查询可能出现 |

### PagedDownloads

```json
{
  "items": [],
  "total": 0,
  "page": 1,
  "pageSize": 20
}
```

### InspectResult

```json
{
  "platform": "youtube",
  "normalizedUrl": "https://www.youtube.com/watch?v=abc",
  "videoId": "abc",
  "title": "视频标题",
  "thumbnailUrl": "https://example.com/cover.jpg",
  "qualityLabel": "1080p",
  "container": "mp4",
  "durationSeconds": 120,
  "estimatedSizeBytes": 104857600,
  "suggestedFilename": "视频标题 [abc].mp4",
  "duplicateOf": null
}
```

### Settings

```json
{
  "bindHost": "0.0.0.0",
  "port": 12225,
  "downloadDir": "F:\\code2\\DownGo\\data\\downloads",
  "concurrentDownloads": 2,
  "ytDlpPath": "F:\\code2\\DownGo\\data\\bin\\yt-dlp.exe",
  "ffmpegPath": "F:\\code2\\DownGo\\data\\bin\\ffmpeg.exe",
  "bilibiliMid": 0,
  "bilibiliUname": "",
  "bilibiliFace": "",
  "bilibiliFaceLocal": "",
  "bilibiliLoginAt": "",
  "bilibiliCheckStatus": "missing",
  "bilibiliCheckedAt": "",
  "bilibiliLevel": 0,
  "bilibiliSex": "",
  "bilibiliSign": "",
  "bilibiliVipStatus": 0,
  "bilibiliVipType": 0,
  "bilibiliVipLabel": "",
  "bilibiliVipDueDate": 0,
  "bilibiliSeniorMember": false
}
```

## Public 接口

Public 接口不需要鉴权，适合外部系统接入。

### 创建下载任务

```http
POST /api/public/downloads
```

请求体：

```json
{
  "url": "https://www.youtube.com/watch?v=abc"
}
```

成功响应：`201 Created`

```json
{
  "id": 1,
  "sourceUrl": "https://www.youtube.com/watch?v=abc",
  "status": "resolving"
}
```

可能状态码：

| 状态码 | 说明 |
| --- | --- |
| `201` | 创建成功 |
| `400` | URL 不合法、依赖缺失或解析失败 |
| `409` | 任务已存在，响应包含 `code: DOWNLOAD_ALREADY_EXISTS` 和 `item` |

### 查询进行中任务

```http
GET /api/public/downloads/progress?page=1&pageSize=20
```

成功响应：`200 OK`，返回 `PagedDownloads`。

该接口返回状态为 `resolving`、`queued`、`downloading`、`postprocessing`、`failed` 的任务。

### 查询已完成任务

```http
GET /api/public/downloads/completed?page=1&pageSize=20
```

成功响应：`200 OK`，返回 `PagedDownloads`。

该接口只返回状态为 `completed` 的任务。

### 删除已完成文件

```http
DELETE /api/public/downloads/{id}
```

兼容别名：

```http
DELETE /api/public/downloads/{id}/file
```

成功响应：`204 No Content`

行为：

- 仅允许删除 `status=completed` 的任务。
- 会删除数据库中的任务可见记录、下载完成文件、同名前缀的关联临时/元数据文件，以及本地缓存封面。
- 未完成任务不会被删除。

可能状态码：

| 状态码 | 说明 |
| --- | --- |
| `204` | 删除成功 |
| `400` | 任务不是已完成状态 |
| `404` | 任务不存在 |

## 鉴权

### 登录

```http
POST /api/auth/login
```

请求体：

```json
{
  "password": "访问密码"
}
```

成功响应：`200 OK`

```json
{
  "token": "jwt-or-random-token"
}
```

可能状态码：

| 状态码 | 说明 |
| --- | --- |
| `200` | 登录成功 |
| `400` | 请求体格式错误 |
| `401` | 密码错误 |

## 设置接口

以下接口需要鉴权。

### 获取设置

```http
GET /api/settings
```

成功响应：`200 OK`，返回 `Settings`。

### 更新设置

```http
PUT /api/settings
```

请求体：

```json
{
  "bindHost": "0.0.0.0",
  "port": 12225,
  "downloadDir": "F:\\Downloads",
  "concurrentDownloads": 2,
  "ytDlpPath": "F:\\code2\\DownGo\\data\\bin\\yt-dlp.exe",
  "ffmpegPath": "F:\\code2\\DownGo\\data\\bin\\ffmpeg.exe",
  "accessPassword": "新访问密码"
}
```

成功响应：`200 OK`，返回更新后的 `Settings`。

说明：

- `accessPassword` 为空时不会修改访问密码。
- 当前后端更新逻辑会处理 `bindHost`、`port`、`downloadDir`、`concurrentDownloads`、`accessPassword`。

### 选择下载目录

```http
POST /api/settings/download-dir/select
```

请求体：

```json
{
  "currentDir": "F:\\Downloads"
}
```

成功响应：

- `200 OK`：用户选择了目录

```json
{
  "path": "F:\\Videos"
}
```

- `204 No Content`：用户取消选择

## 下载任务接口

以下接口需要鉴权。

### 预解析下载链接

```http
POST /api/downloads/inspect
```

请求体：

```json
{
  "url": "https://www.youtube.com/watch?v=abc"
}
```

成功响应：`200 OK`，返回 `InspectResult[]`。

### 创建下载任务

```http
POST /api/downloads
```

请求体：

```json
{
  "url": "https://www.youtube.com/watch?v=abc"
}
```

成功响应：`201 Created`，返回 `DownloadItem`。

### 查询下载任务列表

```http
GET /api/downloads?view=active&page=1&pageSize=20
```

查询参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `view` | 否 | `active` 或 `completed`，默认 `active` |
| `page` | 否 | 页码，默认 `1` |
| `pageSize` | 否 | 每页条数，默认 `20` |

成功响应：`200 OK`，返回 `PagedDownloads`。

### 下载任务事件流

```http
GET /api/downloads/events
```

响应类型：`text/event-stream`

事件格式：

```text
data: {"type":"updated","item":{"id":1,"status":"downloading","progressPercent":45}}
```

事件字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `type` | string | `created`、`updated`、`removed` |
| `item` | DownloadItem | 下载任务 |

### 重试失败任务

```http
POST /api/downloads/{id}/retry
```

成功响应：`200 OK`，返回 `DownloadItem`。

说明：只有 `failed` 状态的任务可以重试。

### 打开文件所在位置

```http
POST /api/downloads/{id}/open-path
```

成功响应：`204 No Content`

说明：只支持已完成且本地文件存在的任务。

### 删除任务

```http
DELETE /api/downloads/{id}
```

成功响应：`204 No Content`

行为：

- 进行中的任务会先取消下载进程。
- 会删除输出文件、关联临时/元数据文件和本地缓存封面。
- 数据库记录会被软删除。

### 获取缩略图

```http
GET /api/downloads/{id}/thumbnail
```

成功响应：`200 OK`，返回图片文件。

说明：需要鉴权，可使用 `Authorization` 或 `?token=`。

### 获取下载文件

```http
GET /api/downloads/{id}/file
```

成功响应：`200 OK`，返回本地下载文件。

可能状态码：

| 状态码 | 说明 |
| --- | --- |
| `200` | 返回文件 |
| `400` | 文件尚未下载完成 |
| `404` | 任务不存在 |

## Bilibili 接口

以下接口需要鉴权。

### 生成登录二维码

```http
POST /api/bilibili/qrcode
```

成功响应：`200 OK`

```json
{
  "url": "https://passport.bilibili.com/...",
  "qrcodeKey": "..."
}
```

### 轮询二维码登录状态

```http
GET /api/bilibili/qrcode/poll?key=<qrcodeKey>
```

成功响应：`200 OK`

```json
{
  "code": 0,
  "message": "0",
  "session": {
    "loggedIn": true,
    "mid": 123,
    "uname": "用户名",
    "face": "https://...",
    "faceLocal": "/api/bilibili/avatar",
    "loginAt": "2026-05-11T10:30:00Z",
    "status": "unchecked",
    "checkedAt": "",
    "message": "",
    "level": 0,
    "sex": "",
    "sign": "",
    "vipStatus": 0,
    "vipType": 0,
    "vipLabel": "",
    "vipDueDate": 0,
    "seniorMember": false
  }
}
```

### 获取 Bilibili 登录会话

```http
GET /api/bilibili/session
```

成功响应：`200 OK`，返回 Bilibili session 对象。

### 检查 Bilibili 登录状态

```http
POST /api/bilibili/session/check
```

成功响应：`200 OK`，返回 Bilibili session 对象。

### 清除 Bilibili 登录会话

```http
DELETE /api/bilibili/session
```

成功响应：`204 No Content`

### 获取 Bilibili 头像

```http
GET /api/bilibili/avatar
```

成功响应：`200 OK`，返回头像文件。

### 获取收藏夹列表

```http
GET /api/bilibili/favorites/folders
```

成功响应：`200 OK`

```json
[
  {
    "id": 123,
    "title": "默认收藏夹"
  }
]
```

### 获取收藏夹订阅配置

```http
GET /api/bilibili/favorites/subscription
```

成功响应：`200 OK`

```json
{
  "mediaId": 123,
  "title": "默认收藏夹",
  "enabled": true,
  "lastCheckedAt": "2026-05-11T10:30:00Z",
  "lastError": "",
  "updatedAt": "2026-05-11T10:30:00Z"
}
```

### 更新收藏夹订阅配置

```http
PUT /api/bilibili/favorites/subscription
```

请求体：

```json
{
  "mediaId": 123,
  "title": "默认收藏夹",
  "enabled": true
}
```

成功响应：`200 OK`，返回收藏夹订阅配置。

### 立即执行收藏夹订阅

```http
POST /api/bilibili/favorites/subscription/run
```

成功响应：`200 OK`，返回收藏夹订阅配置。

## 工具依赖接口

以下接口需要鉴权。

### 获取依赖状态

```http
GET /api/tools/dependencies
```

成功响应：`200 OK`

```json
{
  "binDir": "F:\\code2\\DownGo\\data\\bin",
  "ytDlp": {
    "path": "F:\\code2\\DownGo\\data\\bin\\yt-dlp.exe",
    "exists": true,
    "downloaded": false
  },
  "ffmpeg": {
    "path": "F:\\code2\\DownGo\\data\\bin\\ffmpeg.exe",
    "exists": true,
    "downloaded": false
  }
}
```

### 安装缺失依赖

```http
POST /api/tools/dependencies/install
```

成功响应：`200 OK`，返回安装快照：

```json
{
  "installing": true,
  "events": {},
  "status": {
    "binDir": "F:\\code2\\DownGo\\data\\bin",
    "ytDlp": {
      "path": "F:\\code2\\DownGo\\data\\bin\\yt-dlp.exe",
      "exists": false,
      "downloaded": false
    },
    "ffmpeg": {
      "path": "F:\\code2\\DownGo\\data\\bin\\ffmpeg.exe",
      "exists": true,
      "downloaded": false
    }
  }
}
```

### 获取依赖安装状态

```http
GET /api/tools/dependencies/install/status
```

成功响应：`200 OK`，返回安装快照。

### 依赖安装事件流

```http
GET /api/tools/dependencies/install/events
```

响应类型：`text/event-stream`

事件格式：

```text
data: {"type":"progress","name":"yt-dlp.exe","bytes":1048576,"totalBytes":5242880,"percent":20}
```

事件类型：

| 类型 | 说明 |
| --- | --- |
| `started` | 开始下载依赖 |
| `progress` | 下载进度 |
| `skipped` | 文件已存在，跳过 |
| `completed` | 单个依赖下载完成 |
| `failed` | 单个依赖下载失败 |
| `done` | 本轮安装流程结束 |

## 状态码汇总

| 状态码 | 说明 |
| --- | --- |
| `200 OK` | 请求成功并返回 JSON 或文件 |
| `201 Created` | 资源创建成功 |
| `204 No Content` | 请求成功，无响应体 |
| `400 Bad Request` | 请求体错误、参数错误或业务校验失败 |
| `401 Unauthorized` | 缺少 token 或 token 无效 |
| `404 Not Found` | 资源不存在 |
| `409 Conflict` | 下载任务重复 |
| `500 Internal Server Error` | 服务端内部错误 |

## 调用示例

### Public 创建并查询任务

```powershell
$base = "http://127.0.0.1:12225"
Invoke-RestMethod "$base/api/public/downloads" -Method Post -ContentType "application/json" -Body '{"url":"https://www.youtube.com/watch?v=abc"}'
Invoke-RestMethod "$base/api/public/downloads/progress?page=1&pageSize=20"
Invoke-RestMethod "$base/api/public/downloads/completed?page=1&pageSize=20"
```

### Public 删除已完成文件

```powershell
$base = "http://127.0.0.1:12225"
Invoke-RestMethod "$base/api/public/downloads/1" -Method Delete
```

### 登录后调用受保护接口

```powershell
$base = "http://127.0.0.1:12225"
$login = Invoke-RestMethod "$base/api/auth/login" -Method Post -ContentType "application/json" -Body '{"password":"your-password"}'
$headers = @{ Authorization = "Bearer $($login.token)" }
Invoke-RestMethod "$base/api/downloads?view=active" -Headers $headers
```

## Public system metrics

```http
GET /api/public/system/metrics
```

No token is required. The endpoint returns a real-time snapshot for polling clients, including CPU, memory, disks, network interfaces, host information, DownGo process runtime stats, and optional per-group `errors`.

Network interface entries include traffic counters and IP information:

```json
{
  "network": {
    "interfaces": [
      {
        "name": "Ethernet",
        "hardwareAddr": "00-11-22-33-44-55",
        "mtu": 1500,
        "flags": ["up", "broadcast", "multicast"],
        "isUp": true,
        "ipAddresses": [
          {
            "address": "192.168.1.10",
            "family": "ipv4",
            "cidr": "192.168.1.10/24"
          },
          {
            "address": "fe80::1234",
            "family": "ipv6",
            "cidr": "fe80::1234/64"
          }
        ],
        "bytesSent": 123456,
        "bytesRecv": 987654,
        "packetsSent": 100,
        "packetsRecv": 120
      }
    ]
  }
}
```

Example:

```powershell
$base = "http://127.0.0.1:12225"
Invoke-RestMethod "$base/api/public/system/metrics"
```

## Public disk information

```http
GET /api/public/system/disks
```

No token is required. Returns physical disks with cached temperature information and optional per-group `errors`.

This endpoint returns physical disks, not partitions or drive-letter volumes. For example, an NVMe SSD is returned as one disk even if it contains `C:\` and `D:\`.

Success response: `200 OK`

```json
{
  "timestamp": "2026-05-12T10:30:00Z",
  "physicalDisks": [
    {
      "deviceId": "0",
      "friendlyName": "Samsung SSD 980",
      "serialNumber": "S64...",
      "mediaType": "SSD",
      "busType": "NVMe",
      "healthStatus": "Healthy",
      "operationalStatus": "OK",
      "sizeBytes": 1000204886016,
      "temperatureCelsius": 39,
      "temperatureUpdatedAt": "2026-05-12T10:00:00Z",
      "temperatureError": ""
    }
  ],
  "temperatureUpdatedAt": "2026-05-12T10:00:00Z",
  "nextRefreshAt": "2026-05-12T10:30:00Z",
  "errors": {
    "physicalDisks": "optional warning or collection error"
  }
}
```

Field notes:

| Field | Description |
| --- | --- |
| `physicalDisks` | Physical disk devices from the server, not partitions |
| `temperatureCelsius` | Latest cached disk temperature; may be `null` when unsupported |
| `temperatureUpdatedAt` | Time of the cached temperature sample |
| `nextRefreshAt` | Next scheduled temperature refresh time |
| `temperatureError` | Per-disk reason when temperature is unavailable |
| `errors` | Optional collector warnings/errors; successful groups are still returned |

```http
GET /api/public/system/disk-temperatures
```

No token is required. Returns the latest cached disk temperature snapshot. DownGo refreshes disk temperatures in the background every 30 minutes.

Success response: `200 OK`

```json
{
  "updatedAt": "2026-05-12T10:00:00Z",
  "nextRefreshAt": "2026-05-12T10:30:00Z",
  "items": [
    {
      "deviceId": "0",
      "friendlyName": "Samsung SSD 980",
      "serialNumber": "S64...",
      "temperatureCelsius": 39,
      "temperatureError": "",
      "updatedAt": "2026-05-12T10:00:00Z"
    }
  ],
  "errors": {
    "physicalDisks": "optional warning or collection error"
  }
}
```

Temperature behavior:

- DownGo refreshes temperature data once at startup and then every 30 minutes.
- Querying `/api/public/system/disks` or `/api/public/system/disk-temperatures` does not force a temperature refresh.
- On Windows, collection uses PowerShell in a hidden window. Devices that do not expose temperature still appear, with `temperatureCelsius: null`.

Example:

```powershell
$base = "http://127.0.0.1:12225"
Invoke-RestMethod "$base/api/public/system/disks"
Invoke-RestMethod "$base/api/public/system/disk-temperatures"
```

## Public partition usage

```http
GET /api/public/system/partitions
```

No token is required. Returns partitions, drive letters, or mount points with capacity and usage information.

This endpoint is different from `/api/public/system/disks`: `/system/disks` returns physical disks, while `/system/partitions` returns mounted partitions such as `C:\` and `D:\`.

Success response: `200 OK`

```json
{
  "timestamp": "2026-05-12T10:30:00Z",
  "items": [
    {
      "path": "C:\\",
      "fstype": "NTFS",
      "totalBytes": 512110190592,
      "usedBytes": 301000000000,
      "freeBytes": 211110190592,
      "usedPercent": 58.8
    }
  ],
  "errors": {
    "partition:X:\\": "optional warning or collection error"
  }
}
```

Example:

```powershell
$base = "http://127.0.0.1:12225"
Invoke-RestMethod "$base/api/public/system/partitions"
```

## Public thumbnail access

`GET /api/public/downloads/completed` returns `thumbnailUrl` as `/api/public/downloads/{id}/thumbnail` when a completed item has a locally cached thumbnail. Remote thumbnail URLs remain unchanged.

```http
GET /api/public/downloads/{id}/thumbnail
```

Success response: `200 OK`, returns the image file. Missing, unfinished, or non-cached thumbnails return `404`.
