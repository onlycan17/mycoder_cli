# CLI UX 명세

## 명령어
- `mycoder chat` : 대화형 모드(SSE 스트리밍, 인용 표시).
- `mycoder ask "<질문>" [--project <id>] [--k 5]` : 일회성 Q&A(RAG 컨텍스트 포함).
- `mycoder chat "<프롬프트>" [--project <id>] [--k 5]` : 스트리밍 대화(RAG 컨텍스트 포함).
  - 스트리밍 이벤트: `token`(증분 텍스트), `error`(메시지), `done`(종료)
  - Ctrl‑C 시 스트림 중단(서버 취소 전파)
- `mycoder explain <path|symbol>` : 파일/심볼 설명.
- `mycoder edit --goal "<설명>" [--files ...]` : 패치 제안→미리보기→적용.
- `mycoder test [--target <pkg|path>]` : 테스트 실행.
- `mycoder index [--full|--incremental]` : 인덱싱 수행.
  - 옵션: `--max-files`, `--max-bytes`, `--include '<glob,glob>'`, `--exclude '<glob,glob>'`
  - `--stream` 사용 시 진행상황 스트리밍(SSE). 이벤트에 따라 `job`, `progress indexed/total`, `completed` 표시
  - Ctrl‑C 시 진행 스트림 중단 및 서버 취소 전파
- `mycoder knowledge add <url|file>` : 외부 지식 추가.
- `mycoder search "<쿼리>"` : 의미+단어 검색 결과 출력.
- `mycoder plan "<작업>"` : 단계별 계획 생성.
- `mycoder hooks run` : `make fmt-check && make test && make lint` 실행. `--targets`/`--timeout`/`--verbose` 지원, 실패 시 요약과 힌트(suggestion) 출력.
- `mycoder projects [list|create]` : 프로젝트 조회/생성(`--name`, `--root`).
- `mycoder models` : LLM 서버의 `/v1/models` 목록 조회.
  - 옵션: `--format table|json|raw`(기본 table), `--filter <substr>`, `--color`
- `mycoder metrics` : 서버 `/metrics` 출력(기본 Prometheus 텍스트, `?format=json` 지원).
  - 옵션: `--json`(JSON pretty), `--color`(텍스트 모드 키 컬러)
- `mycoder knowledge add --project <id> --type <code|doc|web> --text "..." [--title ...] [--url ...]`
- `mycoder knowledge list --project <id>`
- `mycoder knowledge vet --project <id>`
- `mycoder knowledge promote --project <id> --title "..." --text "..." [--url ...] [--commit ...] [--pin]`
- `mycoder knowledge reverify --project <id>`
- `mycoder knowledge gc --project <id> [--min 0.5]`
- `mycoder knowledge promote-auto --project <id> --files "path/a.go,path/b.go" [--title ...] [--pin]`: 코드 파일 요약 후 자동 승격

## 파일/터미널/MCP
- `mycoder exec -- -- <cmd> [args...]` : 터미널 명령 실행(기본 비스트리밍, `--project`, `--timeout`, `--cwd`, `--env` 지원).
  - 출력 제한(비스트리밍): `--tail N`(마지막 N라인만), `--max-bytes N`(마지막 N바이트만)
  - 스트리밍: `mycoder exec --project <id> --stream -- -- <cmd> [args...]` (SSE: `stdout|stderr|exit`)
    - 스트리밍 요약: `--stream-tail N` 사용 시 종료 후 마지막 N라인만 출력
- `mycoder fs read|write|patch|delete --project <id> --path <p> [--content ...] [--start N --length N --replace ...]` : 프로젝트 루트 내 파일 조작.
  - 안전장치: `--dry-run`(미리보기), `--yes` 없으면 적용 거부(write/delete/patch)
  - 대량 변경 감지: `--large-threshold-bytes`(기본 65536) 초과 변경은 차단, `--allow-large`로 우회 가능
- `mycoder mcp tools` / `mycoder mcp call <tool> --json '<params>'` : MCP 도구 조회/호출.

## 공통 규칙
- 모든 답변은 인용(파일:시작–끝 라인) 포함.
- 디프는 컬러 미리보기 후 적용 여부 확인.
- 실패 시 진단/자동 제안 표시, 재시도 옵션 제공.
- 설정 파일: `~/.mycoder/config.yaml` (프로파일/API 키/백엔드 설정).
 - 서버 주소: `MYCODER_SERVER_URL`(기본 `http://localhost:8089`)
 - LLM 설정(환경변수):
   - LM Studio(기본): `MYCODER_OPENAI_BASE_URL=http://localhost:1234/v1`, `MYCODER_OPENAI_API_KEY=`(빈값 허용)
   - OpenAI(옵션): `MYCODER_OPENAI_BASE_URL=https://api.openai.com/v1`, `MYCODER_OPENAI_API_KEY=...`
 - 보안 플래그: 외부 경로 접근 `--allow-outside-root`, 파괴적 동작은 기본 확인 요청.

## 사용 예시
```bash
mycoder index --full
mycoder ask "이 프로젝트의 HTTP 서버 초기화 코드는?"
mycoder chat "이 파일을 요약해줘: internal/server/server.go"
mycoder projects create --name demo --root .
mycoder search "index run"
# LM Studio 연동(기본)
export MYCODER_OPENAI_BASE_URL=http://192.168.0.227:3620/v1
export MYCODER_OPENAI_API_KEY=
mycoder edit --goal "/internal/api/handler.go 핸들러에 타임아웃 추가"
mycoder hooks run
```
