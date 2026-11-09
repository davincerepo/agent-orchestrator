# 项目级逐小时用量报表（usage project hourly）

分支：`fleet/feat/usage-project-hourly`。基于 `main-fleet`，集成到 `main-fleet`。

## 行为

新增只读端点 `GET /api/v1/usage/projects/{projectId}/sessions`：一次返回项目内**每个有用量记录的会话**，
每会话携带会话事实（id、num、kind、isTerminated、createdAt、incomplete）和按 harness × 模型 ×
**UTC 小时桶**的 token 向量（input / cachedInput / uncachedInput / output，遵循统一口径：
input=完整输入含缓存读，uncached 含缓存写；null=未知）。

用途：guardian-agent-console 的每项目 token 统计页。积分类套餐（如 GLM Coding Plan）按
"缓存命中/未命中/输出 + 请求时段"折算额度，消费端拿到小时桶即可自行套用各家 provider 的时段
系数，AO 本身保持 provider 中立、不做套餐折算。

## 边界

- 只读聚合，复用 `model_usage_events` 既有数据，不新增写入路径。
- 小时桶直接取 `created_at` 文本前缀（时间戳为 UTC 的 Go time 字符串，前 13 字符即
  `YYYY-MM-DD HH`），映射层归一为 `YYYY-MM-DDTHH`。`created_at` 为 NULL 的事件不参与分桶。
- 没有事件的小时不产生行，响应体积随实际活跃度自适应。
- 同一模型不按 measurement_kind 拆行（拆行会双计）；`<synthetic>` 模型在 service 层过滤，与
  会话详情端点一致。
- 未知 projectId 返回空列表（与 `/usage/sessions?projectId=` 行为一致，不做项目存在性校验）。

## 升级注意

- 上游若改动 usage 聚合查询文件 `queries/usage.sql`，本分支新增的
  `ListProjectSessionHourlyUsage` 查询需要跟随解决冲突；查询仅依赖
  `model_usage_events`/`usage_bindings`/`sessions`/`usage_session_integrity` 四表，上游新增
  列不影响。
- OpenAPI 契约同步：`specgen/build.go` 增 `listProjectSessionUsage` 操作，内嵌 `openapi.yaml`
  随 `go generate ./...` 更新。
