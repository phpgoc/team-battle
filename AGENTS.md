# Team Battle 工程约定

## 项目目标

这是一个单体 Go Web 应用，面向两队团战赛事管理。最终交付物应为一个可独立运行的可执行文件，数据写入本地 SQLite 文件，前端静态资源通过 `go:embed` 打包进二进制。

## 技术约束

- 使用 Go 1.22 或更高版本。
- HTTP 使用 Gin。
- SQLite 使用纯 Go 驱动，避免依赖 CGO，保证单文件部署和跨平台编译。
- 前端使用内嵌的 HTML、CSS、JavaScript，不能依赖独立 Node 服务或外部 CDN。
- 数据库启动时执行幂等迁移；不要要求用户手动初始化表结构。
- 默认数据库路径为当前目录下的 `team-battle.sqlite`，支持 `--db` 覆盖。
- 启动参数为 `--db`、`--host`、`--port`；默认值分别为 `./team-battle.sqlite`、`localhost`、`9527`。
- 应用只提供 HTTP，不实现 TLS；生产 HTTPS 由 Nginx 反向代理终止并转发到应用端口。
- 密码必须使用不可逆哈希保存，不能保存明文密码。
- 会话默认使用 HttpOnly Cookie；修改操作必须经过服务端权限校验。

## 角色和权限

- `superadmin`：可以管理所有账号、赛事和比赛状态。
- `user`：非超级管理员账号；账号是否为某场赛事管理员由 `events.manager_id` 决定，是否为参赛选手由 `event_players` 决定。
- 未登录用户只能访问公开赛事实时页和历史页，不能写入任何数据。

## 赛事业务规则

- 每场赛事固定两支队伍，总参赛人数为 6–12 人，两队人数允许不同。
- 创建赛事时允许固定部分选手队伍，其他选手随机分配。
- 每队通过一人一票产生队长，最高票当选；同票由赛事管理员裁决。
- 第一轮为 1v1 抢分赛，默认目标 4 分；第二轮为 KOF，默认目标 5 分。
- 两个目标分数都不能小于 `floor(total_players / 2)`。
- 首轮人工排阵槽位为 `min(team_size, floor(total_players / 2))`，剩余顺序由随机调度生成。
- 首轮随机调度应避免同一选手连续出场，并尽量保证所有选手先出场后再重复。
- KOF 采用胜者留场、败者换人；败方没有可用选手时，从该队已出场选手中随机复活一人。
- 两轮分别产生阶段胜者；如果阶段胜者不同，进入单场大将战。
- 每次比赛状态变化都要写入审计日志，包含操作者、时间、动作和相关数据。
- 比赛进入进行中后，赛事管理员不能修改队伍成员和赛事目标；超级管理员可以覆盖并记录原因。

## 推荐数据边界

核心表包括：`users`、`events`、`teams`、`event_players`、`captain_votes`、`lineup_entries`、`rounds`、`matches`、`event_actions`、`sessions`、`categories` 和 `site_event_names`。

账号只有 `superadmin` 和非超级管理员两层；赛事管理员由 `events.manager_id` 关联，参赛选手由 `event_players` 关联。分类目录在进程内缓存，分类变更后失效并重新加载。分类图标以图片二进制和 MIME 保存，不使用 emoji 字符代替图片。

比赛阶段建议使用：`draft`、`captain_vote`、`lineup_confirm`、`round1`、`round2`、`ace`、`completed`、`cancelled`。

实时赛事统一使用公开 SSE 接口 `GET /api/public/events/:id/stream`，登录用户和访客使用同一个地址并接收完全相同的公开数据。登录状态只影响操作权限，不影响实时推送内容。客户端使用 EventSource 自动重连，不维护登录用户专用的推送通道。公开接口不得返回密码哈希、会话信息或内部管理字段。

## 文件职责

- `event_management.go`：赛事创建、列表、参赛选手和分队管理。
- `events.go`：SSE 实时推送、心跳和公开事件发送。
- `event_view.go`：赛事详情和供页面使用的数据读取。
- 其他后端逻辑按认证、队长排阵、比赛推进和数据库等职责拆分；不要合并进 `main.go`。
- 前端 HTML、CSS、JavaScript 分文件维护，通过 `go:embed` 嵌入同一个可执行文件。

## 修改和验证要求

- 优先保持 API、数据库迁移和前端展示的一致性。
- 任何影响比赛状态机的修改都必须覆盖正常推进、重复提交、非法阶段操作和权限越权场景。
- 修改代码后至少执行 `gofmt`、`go test ./...` 和 `go build ./...`。
- 不要提交数据库文件、运行时会话数据或包含真实密码的配置文件。
