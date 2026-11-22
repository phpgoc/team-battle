# 团战管理工具

**简体中文** · [English](README.en.md) · [한국어](README.ko.md)

单文件部署的团战赛事管理工具。支持第一轮 1v1、KOF、大将战、实时观赛、历史录入和选手战绩。

## 使用

### 启动

```sh
go run . --db test.sqlite
```

`test.sqlite` 是仓库中的演示数据库，管理员和演示选手密码均为 `test1234`。正式使用请使用新的数据库路径。

### 基本流程

1. 超级管理员创建赛事，填写名称、开始时间和赛事管理员。
2. 在赛事设置中添加 6–12 名选手，选择固定队伍或随机分组。
3. 设置第一轮、KOF 和大将战规则，完成分组抽签后开始比赛。
4. 赛事管理员录入每局胜者。KOF 按胜者留场、败者换人推进，双方各胜一轮后抽取大将战。
5. 全部对局完成后，在赛事设置中确认结束。已结束赛事只能由超级管理员删除或恢复。

无需登录即可观看已公开赛事。赛事录入满 6 人后开放直播，比分通过 SSE 自动更新。首页支持按赛事名、日期和选手筛选历史记录。

超级管理员可在账号、赛事管理、网站设置和赛事背景页面维护数据。赛事管理员负责所管理赛事的名单、排阵、录分和结束确认。登录用户可以维护自己的资料和查看战绩。

## 开发

需要 Go 1.22+。前端通过 `go:embed` 打包，不需要 Node 或外部 CDN。SQLite 使用纯 Go 驱动，支持 `CGO_ENABLED=0`。

```sh
gofmt -w cmd internal main.go
go test ./...
go build ./...
```

端到端测试需要 Node.js 20+ 和 npm：

```sh
cd e2e
npm ci
npx playwright install chromium
npm test
```

业务代码和测试在 `internal/app`，迁移在 `internal/migrations`，正式入口在 `cmd/team-battle`，前端在 `internal/app/web`。演示数据库为根目录的 `test.sqlite`，临时数据和测试产物放在 `runtime`。

SQLite 使用 WAL、每条连接 5 秒锁等待和最多 4 个数据库连接。HTTP 服务可以同时接收约 100 个请求，读请求会在连接池中排队，写请求由应用写锁串行执行。30–40 名普通用户的访问规模适合当前配置，实际吞吐取决于请求类型和磁盘。SSE 上限是所有赛事共享的 500 条连接，不等同于数据库并发数。

## 部署

编译并启动：

```sh
CGO_ENABLED=0 go build -trimpath -o team-battle ./cmd/team-battle
./team-battle --db ./runtime/team-battle.sqlite --host 127.0.0.1 --port 9527
```

Windows PowerShell：

```powershell
$env:CGO_ENABLED = '0'
go build -trimpath -o team-battle.exe ./cmd/team-battle
.\team-battle.exe --db .\runtime\team-battle.sqlite
```

首次启动会创建数据库目录、表结构和 `admin` 管理员，初始密码为 `test1234`。启动前自动执行数据库迁移。停止时发送 Ctrl+C、SIGTERM、SIGHUP 或 SIGQUIT，服务会等待现有请求结束后关闭数据库。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--db` | `./runtime/team-battle.sqlite` | SQLite 文件路径 |
| `--host` | `localhost` | HTTP 监听地址 |
| `--port` | `9527` | HTTP 监听端口 |
| `--sse-max-connections` | `500` | 全部赛事共享的 SSE 连接上限 |
| `--language` | `zh` | 操作日志语言，支持 `zh`、`en`、`ko` |
| `--log` | 静默 | `stdout` 输出到标准输出，其他值按文件路径追加写入 |

程序只提供 HTTP。生产环境建议放在 Nginx 后面处理 HTTPS：

日志示例：

```sh
./team-battle --log stdout
./team-battle --log ./runtime/team-battle.log
```

```nginx
client_max_body_size 50m;
location / {
    proxy_pass http://127.0.0.1:9527;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 3600s;
}
```

升级前停止服务并备份数据库及数据库同目录的 `uploads/`。替换可执行文件后重新启动，程序会自动向上迁移。迁移失败会停止启动，回退时恢复整套备份。
