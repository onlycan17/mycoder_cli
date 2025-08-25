# TODO (대분류/중분류/소분류)

## 현재 작업
 - [x] 요청 ID 전파(X-Request-ID): 미들웨어로 수신/생성, 응답 헤더 반영, 구조화 로그 필드(`req_id`) 추가, 단위 테스트 및 문서화
 - [x] 프로메테우스 스타일 `/metrics` 텍스트 포맷 지표 노출(옵션)
 - [x] RAG 다국어/코드 임베딩 전략 초기 적용(문서/설정/임계치 설계)
 - [ ] Qwen3 Coder 30B 최적화: 프롬프트 지시 강화(근거/한국어/모름), 컨텍스트 예산 상향 검토

## 다음 단계
- [ ] VectorStore 검색 스코프에 `model` 필터 추가(혼류 방지)
- [x] embedpipe Upsert 시 Provider/Model 라벨 환경변수화(하드코딩 제거)
- [x] 번역 폴백 경로(한국어→영어) 추가: env 플래그/타임아웃/테스트 포함
- [x] 청킹을 토큰 기준 + 10~15% 오버랩으로 전환(코드 경계 우선)
- [x] 하이브리드 가중치 α 재튜닝 및 리더보드 구성(k@5/10, MRR)
  - 테스트: `internal/rag/retriever/leaderboard_test.go` (α grid 평가 및 최고 α 선택)
- [ ] 컨텍스트 예산/라인 스니펫 길이 튜닝(모델 컨텍스트 윈도우 확인 후 적용)
- [ ] pgvector(Qdrant 대안) ANN 백엔드 실장 및 선택적 마이그레이션
- [ ] 이웃 확장(함수/클래스 경계 기반) 적용
- [ ] (선택) 크로스엔코더/LLM 재랭킹 상위 M 도입
- [x] 서버 안정성: 간단 RPS 레이트 리미팅(전역/경로/아이피 기준, `MYCODER_RATE_LIMIT_RPS`), 429/Retry-After 처리 및 테스트
  - 세분화 설정 추가: `MYCODER_RATE_LIMIT_GLOBAL_RPS`, `MYCODER_RATE_LIMIT_PATH_RPS`, `MYCODER_RATE_LIMIT_IP_RPS` (미설정 시 `MYCODER_RATE_LIMIT_RPS` 폴백)
- [ ] API 문서 보강: `/fs/patch/unified`, `/fs/patch/unified/rollback`, `/fs/diff` 명세 추가 및 예시 요청/응답
- [ ] 요청 추적성: 요청 로그에 `userAgent`, `referer`, `remoteIP` 필드 추가 및 마스킹 규칙 검토
- [ ] 보안/토큰: `MYCODER_API_TOKEN` 스코프 분리(예: `projects:read`, `fs:write`)와 엔드포인트별 체크(옵션)
- [ ] DX: `mycoder metrics --watch`(주기 갱신) 및 간단 TUI(옵션) 추가
- [ ] 성능: 인덱싱 워커 동시성/큐 튜닝 옵션(`MYCODER_INDEX_MAX_WORKERS`) 및 백프레셔 계측

## 완료
- [x] PRD/아키텍처/RAG/API/데이터모델/CLI/테스트·CI/로드맵 초안 작성
- [x] Go 모듈 초기화 및 최소 레이아웃 구성(`cmd/mycoder`, `internal/server`, `internal/version`, `internal/models`)
- [x] Makefile/CI 워크플로(포맷체크/린트/테스트) 추가
- [x] 최소 서버(`/healthz`) 및 CLI 엔트리(serve/version)
- [x] REST 기본 엔드포인트: `/projects`(GET/POST), `/index/run`, `/index/jobs/:id`, `/search`
- [x] CLI 명령: `projects list|create`, `index`, `search`
- [x] 인덱서(파일 워커/텍스트 수집/바이너리 제외) 및 서버 연결
- [x] 인메모리 스토어 + 간이 검색 → SQLite 스토어 + FTS5 검색 도입
- [x] 마이그레이션 스켈레톤(SQLite) 추가 및 스토어 인터페이스 도입
- [x] 기본 유닛 테스트(API/인덱서)

## 대분류: 리포 부트스트랩 & 빌드 체인
- 중분류: Go 모듈/레이아웃
  - [x] `go mod init mycoder`
  - [x] 디렉토리: `cmd/mycoder`, `internal/{server,indexer,store,version,models}` (기타 모듈은 추후 생성)
  - [x] `cmd/mycoder/main.go` (루트 커맨드/버전/기본 명령)
  - [x] `internal/server/server.go` (기본 /healthz 및 REST 핸들러)
- 중분류: Makefile/훅/CI
  - [x] `make fmt-check`, `make lint`(go vet), `make test`
  - [x] pre-commit 훅 스크립트(게이트 통과 시만 커밋)
  - [x] GitHub Actions: CI 워크플로 추가
- 중분류: 설정/로그/메트릭
  - [x] `~/.mycoder/config.yaml` 로드(env override)
  - [x] 구조화 로그(JSON 라인), 민감정보 마스킹 유틸
  - [x] `/metrics`(Prometheus), 기본 지표 등록
  - [x] HTTP 메트릭 라벨 정규화 및 샘플링(`MYCODER_METRICS_SAMPLE_RATE`)

## 대분류: 저장소/데이터 모델
- 중분류: 스키마/마이그레이션(SQLite+FTS5)
  - [x] 테이블: Project, Document, Chunk, TermIndex(FTS5), Run, Conversation*, ExecutionLog
  - [x] Embedding, Patch, Symbol 스키마 추가
  - [x] Knowledge 스키마 추가(SQLite)
  - [x] 마이그레이션 관리(버전, 롤백 일부), 시드 데이터
  - [x] FTS5 인덱스 생성 및 기본 쿼리
- 중분류: VectorStore 인터페이스/어댑터
  - [x] 인터페이스: `Upsert(chunks)`, `Search(embedding,k)`, `Delete(docID)`
  - [x] 로컬: no-op(or sqlite-vec 감지) 구현으로 degrade 가능
  - [x] 프로덕션: pgvector 어댑터 스텁/설정 스키마
- 중분류: DAO/리포지토리
  - [x] 프로젝트/문서/청크 CRUD(기본), 일관된 트랜잭션 래퍼
  - [x] 실행 로그 기록/조회 API
  - [x] 테스트: 테이블 기반 쿼리/마이그레이션 검증

## 대분류: 인덱서
- 중분류: 파일 워커
  - [x] 기본 워커/바이너리 제외/크기 제한
  - [x] Git 인지(`git ls-files`), `.gitignore` 준수
  - [x] 변경 감지(SHA 계산) 토대
  - [x] 증분 인덱싱(sha 기반 삭제/갱신) 완료
  - [x] mtime 활용(파일 변경시간 비교 및 보관)
  - [x] 인덱싱 옵션 확장: `--max-files`/`--max-bytes`/경로 필터(패턴) CLI 전파
- 중분류: 언어 감지/청킹
  - [x] 언어 감지(확장자/마임)
  - [x] 코드 청커(Go/TS/Py 함수/클래스 단위 + 슬라이딩 보완)
  - [x] 문서 청커(헤딩/문단)
- 중분류: 심볼 추출/그래프
  - [x] Go: `go/ast`로 export 심볼 추출 (refs는 후속)
  - [x] TS: 간단 파서(정규식)로 export 심볼 추출
  - [x] 심볼 그래프 저장/조회
- 중분류: 임베딩 파이프라인
  - [x] 배치 업서트, 실패 재시도 큐, 캐시(sha 기준)
  - [x] 임베딩 공급자 추상화(1536 차원 기본)

## 대분류: RAG
- 중분류: 리트리버
  - [x] BM25(FTS5) 상위 K 검색
  - [x] 벡터 검색(KNN) 통합
  - [x] 하이브리드 결합 ∪ 후 리랭크(LLM/규칙)
- 중분류: 지식(승격/정리)
  - [x] Knowledge 엔드포인트 스켈레톤(add/list/vet)
  - [x] trustScore 기반 리랭크 결합(1차: 경로 일치 가중) 및 유니크 K
  - [x] promote/reverify/gc 엔드포인트/CLI 구현
  - [x] promote-auto 엔드포인트/CLI(파일 요약 → 승격)
  - [x] decay/reverify 배치 및 핀/정리 정책 구현
- 중분류: 품질 개선
  - [x] 경로 중복 제거 및 상위 K 유니크 샘플링
  - [x] 코드 블록 예산 자동 조절/중복 제거 강화
  - [ ] 다국어/번역 폴백 루틴(KO→EN→검색→KO) 설계/구현/테스트
  - [ ] 모델 스코프 필터(`project_id+dim+model`) 적용 및 회귀 테스트
  - [ ] 토큰 기반 청킹+오버랩 구현 및 회귀 테스트
  - [ ] CodeXEmbed(코드 전용 임베딩) 공급자 어댑터 추가 및 헬스체크/배치 설정화
  - [ ] 하이브리드 α 재튜닝/신뢰도 가중 조정(실험/측정 포함)
## 대분류: 검색 품질/맥락
- 중분류: 라인 정보/프리뷰
  - [x] 청크 메타 라인 범위 저장 및 응답 startLine/endLine 제공
  - [x] CLI search 파일:라인 표기
- 중분류: 프롬프트 컴포저/인용
  - [x] 인용 형식 `path:start-end` + 캡션/코드 블록
  - [x] 토큰 예산 기반 컨텍스트 선택/중복 제거(파일별 중복 라인지역 제거, 범위 2개 제한)
- 중분류: 쿼리 플래너/의도 분류
  - [x] intent(nav/explain/edit/research) 분류기
  - [x] 플랜별 컨텍스트 수집 규칙(의도별 K 조정)

## 대분류: LLM 통합
- 중분류: 공급자 추상화
  - [x] 인터페이스: `Chat(stream)`, `Embeddings`
  - [x] OpenAI 호환 어댑터(비/스트리밍, 임베딩)
- 중분류: 로컬 LLM(LM Studio)
  - [x] 베이스URL/키 환경변수 지원(`http://localhost:1234/v1` 기본)
  - [x] LM Studio 통합 스모크 테스트(옵트인)
  - [x] 임베딩 로컬 모델 지원 여부 확인 및 폴백 정책
  - [x] OpenAI(옵션) 베이스URL/키 전환 가이드 문서화
  - [ ] Qwen3 Coder 30B 고정 모델 설정 문서화 및 프롬프트 템플릿 조정
 - 중분류: 안정성
  - [x] 최소 간격(MYCODER_LLM_MIN_INTERVAL_MS) 및 429/5xx 재시도 백오프
- 중분류: 스트리밍/SSE
  - [x] 서버: 챗 스트림 이벤트 표준화(`token|error|done`)
  - [x] 클라이언트: 스트리밍 취소(Ctrl‑C) 처리(chat/index/exec)
  - [x] 클라이언트: TTY 스트리밍 UI/중단·재시도 UX 고도화
  - [x] 서버/클라이언트: SSE token/done 스트리밍 기본 구현

## 대분류: 대화 메모리
- 중분류: 슬라이딩/요약/TTL
  - [x] 슬라이딩 윈도우(역할/규칙 우선 포함)
  - [x] 맵-리듀스 요약(결정/근거 경로 보존)
  - [x] TTL/핀/참조 기반 보존 정책, 정리 잡
- 중분류: 세맨틱 캐시
  - [x] 질의 임베딩 캐시/히트율 추적
  - [x] 캐시 무효화/신선도 관리

## 대분류: 도구(TOOLS)
- 중분류: 훅 러너
  - [x] `make fmt-check && make test && make lint` 실행 및 즉시 중단
  - [x] 구조화 로그(필드화)/로그 아카이빙
  - [x] 실패 유형 힌트(suggestion) 기본 제공
  - [x] 실패 유형 진단/가이드 고도화
  - [x] 결과 요약(타겟별 소요시간/라인·바이트 집계) 출력
  - [x] 결과 요약(타겟별 소요시간/라인·바이트 집계) 출력
 - 중분류: 패치 적용기
  - [x] 유니파이드 디프 파서/적용/컬러 미리보기/충돌 처리/롤백
  - [x] 유니파이드 디프 생성(API/CLI)
- 중분류: 파일시스템 도구
  - [x] read/write/delete 엔드포인트 스텁(루트 경계/정규화)
  - [x] patch 엔드포인트(바이트 오프셋 기반 hunks)
  - [x] `--dry-run`/`--yes`
  - [x] 대량 변경 감지/확인 단계(`--large-threshold-bytes`, `--allow-large`)
- 중분류: 터미널 실행기
  - [x] `exec` 기본 POST 실행(프로젝트 루트 cwd, 타임아웃)
  - [x] SSE 스트리밍(`/shell/exec/stream`) 클라이언트/서버 기본 구현
  - [x] 출력 제한(비스트리밍 64KiB 캡, 스트리밍 `limit` 이벤트)
  - [x] env 화이트리스트(`GOFLAGS`,`GOWORK`,`CGO_ENABLED`)
  - [x] 허용·차단 정책(allow/deny regex), 로그 요약(summary 이벤트/바이트·라인)
- 중분류: MCP 클라이언트
  - [x] 도구 목록 조회/스키마 검증/호출
  - [x] 보안 정책(도구 허용목록/토큰 스코프)

## 대분류: REST API
- 중분류: 기본 엔드포인트
  - [x] `/healthz`
  - [x] `/metrics`
  - [x] `/projects`(GET/POST)
  - [x] `/index/run`, `/index/jobs/:id`
- 중분류: 검색/챗/에딧
  - [x] `/search`(lexical-FTS5/메모리)
  - [x] `/chat`(SSE 표준 이벤트), `/edits/plan`, `/edits/apply`
  - [x] `/knowledge`(POST/GET), `/knowledge/vet`(POST)
- 중분류: FS/쉘/MCP
  - [x] `/fs/read|write|delete`(루트 경계)
  - [x] `/fs/patch`(바이트 오프셋 기반)
  - [x] `/shell/exec`(기본 POST 실행)
  - [x] `/shell/exec/stream`(SSE)
  - [x] 쉘/FS 제한/허용·차단 정책(allow/deny regex), 출력 제한
  - [x] `/tools/hooks` (프로젝트 훅 실행 API)
  - [x] `/mcp/tools`, `/mcp/call`
 - 중분류: 인덱싱 스트리밍
  - [x] `/index/run/stream`(SSE 진행 이벤트: job/progress/completed)
- 중분류: 에러/검증/보안
  - [x] 요청 스키마 검증, 에러코드 표준화
  - [x] 토큰/프로파일/아웃바운드 정책 적용

## 대분류: CLI UX
- 중분류: 기본 명령
  - [x] `mycoder projects`, `mycoder index`, `mycoder search`
  - [x] `mycoder ask`, `mycoder chat`
  - [x] `mycoder hooks run`
  - [x] `mycoder explain`, `mycoder edit`
  - [x] `mycoder test`
- 중분류: 파일/쉘/MCP 명령
  - [x] `mycoder fs read|write|patch|delete`
  - [x] `mycoder fs` 옵션: `--dry-run`/`--yes`
  - [x] `mycoder exec -- cmd [args...]`(+ `--timeout`)
  - [x] `mycoder exec --stream`(SSE 소비)
  - [x] `mycoder exec` 옵션: `--cwd`/`--env`
  - [x] `mycoder mcp tools|call`
 - 중분류: 인덱싱 진행 표시
  - [x] `mycoder index --stream` 진행상황 표시(job/progress/completed)
 - 중분류: 기본 명령 출력 개선
  - [x] `mycoder models` 출력 옵션(`--format|--filter|--color`)
  - [x] `mycoder metrics` 출력 옵션(`--json|--color`)
 - 중분류: 출력/스트리밍
  - [x] 인용/파일:라인 표시, 컬러 디프, 실패 진단/제안
 - [x] 스트림 중단(Ctrl‑C 취소)
  - [x] 스트림 재시도/로그 저장 옵션

## 대분류: 웹 보강(옵션)
- 중분류: 검색/수집/요약
  - [x] 검색 API 연동, 결과 스코어/중복 제거
  - [x] 요약/정규화, 출처/TTL/신뢰점수 저장
  - [x] 수동 승인/핀 기능

## 대분류: 관측/보안/성능
- 중분류: 관측
  - [ ] 트레이스ID/구조화 로그/지표 계측
  - [ ] 실행 로그/아티팩트 보관
- 중분류: 보안
  - [ ] 비밀 마스킹, 외부 호출 옵트인, 경계 강화 테스트
- 중분류: 성능
  - [ ] 인덱싱/검색/LLM 파이프라인 벤치, 캐시 정책 튜닝

## 대분류: QA & 배포
- 중분류: 테스트
  - [ ] 단위/계약/골든/e2e 시나리오 작성
  - [ ] 보안/권한/제한 관련 회귀 테스트
- 중분류: 배포
  - [ ] `goreleaser` 설정(다중 OS/ARCH, 압축 아티팩트)
  - [ ] Homebrew Formula, 설치 스크립트, 사용 가이드

## 결정 사항(요약)
- 언어: Go
- 벡터 스토어 권장: 프로덕션=PostgreSQL+pgvector(HNSW), 로컬=SQLite+FTS5(+sqlite-vec 선택)
- [x] FTS5 검색 고도화(프로젝트 필터/청크 색인/프리뷰)

## 제안/아이디어(모든 TODO 완료 후 검토)
- 스트리밍 진행바: CLI에서 한 줄 갱신(progress bar)로 출력 단순화(chat/index/exec)
- 스트리밍 재시도/복구: SSE 끊김 시 자동 재연결/재시작 전략
- 스트림 로그 보존: `--save-log`(경로)로 원시 로그 아카이빙
- `/metrics` 확장: 지연 히스토그램/요청 크기 메트릭, 라벨 정규화 추가 패턴
- exec 정책 프리셋: 위험 커맨드 기본 차단 프리셋 제공 + 샘플 정책 번들
- MCP 통합 백로그: 도구 카탈로그/스키마 검증/보안정책 정리
