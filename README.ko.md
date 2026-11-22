# 팀 배틀

[简体中文](README.md) · [English](README.en.md) · **한국어**

1라운드 1대1, KOF, 에이스전, 실시간 관전, 과거 경기 입력과 선수 전적을 지원하는 단일 실행 파일 대회 관리 도구입니다.

## 사용

### 시작

```sh
go run . --db test.sqlite
```

저장소의 `test.sqlite`는 데모 데이터베이스입니다. 관리자와 데모 선수의 비밀번호는 `test1234`입니다. 운영 환경에서는 새 데이터베이스 경로를 사용하세요.

### 기본 흐름

1. 슈퍼 관리자가 이름, 시작 시간, 담당자를 지정해 대회를 만듭니다.
2. 선수 6–12명을 추가하고 고정 팀 또는 무작위 팀 배정을 선택합니다.
3. 1라운드, KOF, 에이스 규칙을 설정하고 추첨을 완료한 뒤 시작합니다.
4. 담당자가 각 경기의 승자를 기록합니다. KOF는 승자가 남고 패자가 교체됩니다. 양 팀이 한 라운드씩 이기면 에이스전을 추첨합니다.
5. 모든 경기가 끝나면 대회를 확정합니다. 종료된 대회의 삭제와 복원은 슈퍼 관리자만 할 수 있습니다.

공개 대회는 로그인 없이 볼 수 있습니다. 선수 6명이 입력되면 관전이 열리고 점수는 SSE로 갱신됩니다. 홈 화면에서 대회명, 날짜, 선수로 기록을 필터링할 수 있습니다.

슈퍼 관리자는 계정, 대회, 사이트 설정과 배경을 관리합니다. 대회 담당자는 해당 대회의 명단, 배치, 결과와 종료 확정을 관리합니다. 로그인한 사용자는 자신의 프로필과 전적을 관리할 수 있습니다.

## 개발

Go 1.22 이상이 필요합니다. 프론트엔드는 `go:embed`로 포함되며 Node 서비스와 외부 CDN이 필요하지 않습니다. SQLite는 순수 Go 드라이버를 사용하고 `CGO_ENABLED=0`을 지원합니다.

```sh
gofmt -w cmd internal main.go
go test ./...
go build ./...
```

E2E 테스트에는 Node.js 20 이상과 npm이 필요합니다.

```sh
cd e2e
npm ci
npx playwright install chromium
npm test
```

애플리케이션 코드와 테스트는 `internal/app`, 마이그레이션은 `internal/migrations`, 실행 진입점은 `cmd/team-battle`, 프론트엔드는 `internal/app/web`에 있습니다. 데모 데이터베이스는 `test.sqlite`이며 임시 데이터와 테스트 결과는 `runtime`에 둡니다.

SQLite는 WAL, 모든 연결의 5초 잠금 대기, 최대 4개의 데이터베이스 연결을 사용합니다. HTTP 서버는 약 100개의 동시 요청을 받을 수 있습니다. 읽기 요청은 풀에서 대기하고 쓰기 요청은 애플리케이션 잠금으로 직렬화됩니다. 현재 설정은 일반 사용자 30–40명 규모에 맞습니다. 실제 처리량은 요청 종류와 디스크 속도에 따라 달라집니다. SSE 상한 500은 모든 대회가 공유하며 데이터베이스 동시성과 별개입니다.

## 배포

빌드와 실행:

```sh
CGO_ENABLED=0 go build -trimpath -o team-battle ./cmd/team-battle
./team-battle --db ./runtime/team-battle.sqlite --host 127.0.0.1 --port 9527
```

첫 실행 시 데이터베이스 디렉터리와 스키마를 만들고 비밀번호 `test1234`인 `admin` 계정을 생성합니다. HTTP를 열기 전에 마이그레이션을 실행합니다. Ctrl+C, SIGTERM, SIGHUP, SIGQUIT을 받으면 진행 중인 요청을 마친 뒤 데이터베이스를 닫습니다.

| 옵션 | 기본값 | 설명 |
| --- | --- | --- |
| `--db` | `./runtime/team-battle.sqlite` | SQLite 파일 |
| `--host` | `localhost` | HTTP 수신 주소 |
| `--port` | `9527` | HTTP 수신 포트 |
| `--sse-max-connections` | `500` | 전체 대회의 SSE 연결 상한 |
| `--language` | `zh` | 작업 로그 언어: `zh`, `en`, `ko` |
| `--log` | quiet | `stdout`은 표준 출력, 그 외 값은 파일 경로로 추가 기록 |

프로그램은 HTTP만 제공합니다. HTTPS는 Nginx에서 처리하세요.

로그 예시:

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

업그레이드 전에 서비스를 중지하고 데이터베이스와 같은 디렉터리의 `uploads/`를 백업하세요. 실행 파일을 교체하고 다시 시작하면 상위 마이그레이션이 자동 적용됩니다. 마이그레이션 실패 시 시작하지 않습니다. 롤백할 때는 전체 백업을 복원하세요.
