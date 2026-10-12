# 图片与视频生成统一接入协议

适用于 ZayuAPI 的图片生成、视频生成工作台与直接 API 调用。站点 API 使用本站 API 密钥，账号内的上游密钥仅由服务器向该账号配置的上游发送。示例中的 `YOUR_SITE`、`YOUR_KEY`、`MODEL_ID` 均需替换。

## 认证与模型

```http
Authorization: Bearer YOUR_KEY
Content-Type: application/json
```

`GET https://YOUR_SITE/v1/models` 获取当前密钥可用的模型。工作台还要求分组声明对应的图片或视频功能。不要将供应商密钥填写到用户侧密钥栏。

## 图片

| 操作 | 接口 |
| --- | --- |
| 同步生成 | `POST /v1/images/generations` |
| 同步编辑 | `POST /v1/images/edits` |
| 异步生成 | `POST /v1/images/generations/async` |
| 异步编辑 | `POST /v1/images/edits/async` |
| 查询异步结果 | `GET /v1/images/tasks/{task_id}` |

```json
{
  "model": "MODEL_ID",
  "prompt": "一片平静的湖面，白天，自然光",
  "n": 1,
  "size": "1024x1024",
  "response_format": "url"
}
```

编辑接口支持 `multipart/form-data`：`model`、`prompt`、`size` 等文本字段，加上 `image` / `image[]` 文件字段。JSON 编辑使用 `images: [{"image_url": "https://..."}]`。

同步成功返回 OpenAI Images 格式 `data: [{"url": "..."}]` 或 `b64_json`；异步接口返回任务 ID，客户端轮询任务接口，完成后读取 `result.data`。工作台选择多张时创建多个独立的单图任务，并按用户并发限制执行，其余排队。

968 账号可使用 `size: "16:9"`、`resolution: "1k" | "2k" | "4k"` 和 `image_urls`。本站也会将工作台像素尺寸及上传参考图转换为其协议。968 每次仅支持 `n: 1`，最多 8 张参考图，单张不超过 20 MB；本站管理员设置的更小限额仍然生效。

## 视频

本站以 OpenAI Sora 风格统一提供视频任务接口：

| 操作 | 接口 |
| --- | --- |
| 创建任务 | `POST /v1/videos`，别名 `POST /v1/videos/generations` |
| 查询任务 | `GET /v1/videos/{id}` |
| 下载视频 | `GET /v1/videos/{id}/content` |

```json
{
  "model": "MODEL_ID",
  "prompt": "湖面轻微波动，固定镜头，无文字",
  "seconds": "4",
  "size": "854x480",
  "resolution": "480p",
  "aspect_ratio": "16:9"
}
```

一次请求创建一个视频。图片引导可使用 `input_reference: {"image_url": "https://..."}`，Sora 账号也支持上游原生 multipart `input_reference` 文件。`seconds` 与 `duration` 不要提供相互矛盾的值。

工作台内部使用 `POST /v1/video-workbench/tasks`，字段为 `model`、`prompt`、`duration`（整数秒）、`ratio`、`resolution`，以及可选的 `referenceImages`、`referenceVideos`、`referenceAudios`。此接口另需工作台用户身份校验，不建议第三方客户端代替上述公共 API 使用。

宽高比默认 `1:1`。分辨率为 `auto`、`480p`、`720p`、`1080p`、`2k`、`4k`，默认 `auto`。自动模式省略上游分辨率限制，不代表每个模型都支持所有比例、时长或档位。原生 Sora 模型仅支持其官方尺寸和时长；不支持的组合会被拒绝。

创建成功返回任务 `id`。处理状态可能为 `queued`、`pending`、`in_progress`、`processing`；成功为 `completed`、`succeeded`、`done`；失败为 `failed`、`cancelled`、`canceled` 等。上游额外字段保留，客户端应兼容未知字段。建议每 30 秒查询一次，不要通过再次创建来查询任务。

下载始终调用本站带认证的内容接口。服务器获取上游受保护文件；转向第三方 CDN 时不携带上游密钥，并校验地址和重定向。任务查询和下载仅允许任务所属用户、密钥访问。

## 账号协议

在 Custom 账号创建或编辑中选择“图片与视频协议”：

| 协议 | 创建视频 | 查询视频 | 参考资料 |
| --- | --- | --- | --- |
| 默认兼容协议 | `/v1/videos` | `/v1/videos/{id}` | 保留既有透传和 MiniMax 工作台适配 |
| OpenAI Sora | `/v1/videos` | `/v1/videos/{id}` | `seconds`、`size`、`input_reference`；Grok 兼容模型允许分辨率及比例扩展 |
| 968 | `/v1/videos/generations` | `/v1/videos/generations/{id}` | `duration`、`resolution`、`aspect_ratio`、`image_refs` / `video_refs` / `audio_refs` |

Base URL 可带或不带 `/v1`，不会重复拼接版本路径。协议选择是账号级配置，不会在付费创建失败后自动切换路径重发。

968 视频结果可能包含 `video.url` 和 `video.duration`，结算优先采用实际上游时长。其可用模型以 `/models` 为准；接口文档列出模型不代表当前密钥已开通。

## 预扣费与退款

生成请求真正执行、发送给上游之前，本站按分组或渠道定价预扣余额及相应配额。数据库事务锁保证不同密钥、并发请求和多服务实例不能重复花费同一笔余额。余额或配额不足时不调用付费上游。

工作台排队与实际并发分开：等待中的任务不占用上游线程，到执行时重新校验权限和可用资金。价格未配置或分辨率没有可用定价时拒绝发起付费请求；自动档位需要配置默认视频价格。

成功后以实际图片输出或视频用量结算，预扣款用于抵扣，差额退回或补扣，不会再重复扣整笔。失败任务退回预扣余额及本次占用的配额；重复状态查询不会重复扣费或退款。图片按 token 计费时先预留输出预算，成功后按上游实际 token 结算。

异步视频由服务器定期对账，关闭浏览器不影响后续结算；待处理的账本记录保存在数据库，服务重启后恢复查询。已确认的结算、退款结果先保存到数据库，账务更新短暂失败时会继续重试。仅凭预扣记录时间较久不会自动退款；如果进程在上游受理和保存任务号之间中断，需要管理员核对上游记录。

创建请求被明确拒绝时退回本地预扣。已取得任务号后，轮询超时或临时查询错误不会自动退款，也不会自动重新创建；后台继续查询原任务。上游已确认失败则退款，已完成则结算。完整数据库备份包含预扣凭据；恢复旧版本备份时不会沿用恢复前的任务凭据。

生成结果保存时间由管理员工作台设置决定。保存时间到期不退生成费用；云端已生成但本地下载失败也不等于上游生成失败，应使用任务 ID 联系管理员核实。

## 错误与重试

- `400`：字段、模型能力或上传内容错误，修改请求后再提交。
- `401` / `403`：身份、权限、余额或定价不满足要求。
- `429`：并发或配额限制，按返回信息等待。
- `5xx` / 网络超时：先核对任务列表和已有任务 ID，避免自动重发付费创建请求。

已创建任务的查询、下载不再要求剩余余额足够创建新任务，但仍校验密钥身份及任务归属。敏感参数、供应商密钥和真实账号信息不应写进客户端日志或示例。

官方 Sora 字段参考：https://developers.openai.com/api/reference/resources/videos/methods/create

此处兼容的是 OpenAI Sora 风格的 HTTP 协议；本次测试使用用户配置的第三方 Grok 模型服务，不代表 OpenAI 官方 Sora 服务当前可用。图片接口使用 OpenAI Images 格式，Sora 是视频协议。
