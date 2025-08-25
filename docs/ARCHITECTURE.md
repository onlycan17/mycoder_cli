# 아키텍처 명세

## 1. 전체 구조
- 구성요소: CLI ↔ 로컬 데몬(HTTP) ↔ 인덱서 ↔ RAG 서비스 ↔ 지식저장소(SQLite/FTS5 + 벡터 스토어) ↔ LLM 게이트웨이 ↔ 도구(포맷/린트/테스트/웹검색).
- 데이터 흐름: CLI 요청 → 의도 분류 → 하이브리드 검색(BM25+ANN) → 리랭크 → 프롬프트 구성(인용 포함) → LLM 호출 → (도구 실행) → 응답/패치 → 훅 실행 → 로그 기록.

## 2. 모듈
- CLI: 명령 파싱, 스트리밍 출력, 디프 미리보기/확정.
- HTTP 데몬: REST 엔드포인트(`/chat`, `/edits/*`, `/index/*`, `/search`, `/knowledge`, `/projects`, `/tools/hooks`).
- 인덱서: Git 인지 파일 워커, 언어 감지, 언어별 청커, 심볼 추출, 증분 인덱싱.
- RAG: 리트리버(BM25+벡터), 리랭커, 쿼리 플래너(탐색/설명/편집/리서치), 인용자.
  - 다국어/폴백: 한글 질의 직접 검색 → 임계치 미달 시 KO→EN 번역 후 재검색(옵션, env로 토글).
  - 모델 스코프: VectorStore 검색은 `project_id + dim + model` 범위로 제한해 혼류 방지.
- 저장소: Document/Chunk/Embedding/TermIndex/Run/Symbol/Project 테이블, 외부 벡터 인덱스.
- LLM 게이트웨이: 공급자 추상화(OpenAI-호환 로컬/LM Studio 중심), 스트리밍, 재시도/속도제한.
- 도구 실행기: 포맷/린트/테스트 훅, 패치 적용/롤백, 웹 검색 수집, 파일시스템/터미널 실행, MCP 클라이언트.

## 3. 인터페이스(확장점)
- VectorStore, Retriever, Chunker, LLMProvider, Tool, WebSource.
- 의존성 역전: 상위 레이어는 인터페이스만 알고 구현은 주입.

### LLMProvider 구성(환경변수)
- LM Studio(기본):
  - `MYCODER_OPENAI_BASE_URL=http://localhost:1234/v1`
  - `MYCODER_OPENAI_API_KEY=`(빈값/더미 허용)
  - `MYCODER_CHAT_MODEL`, `MYCODER_EMBEDDING_MODEL`
- OpenAI(옵션):
  - `MYCODER_OPENAI_BASE_URL=https://api.openai.com/v1`
  - `MYCODER_OPENAI_API_KEY=sk-...`

### VectorStore 선택(권장 스펙)
- 개발/로컬 기본: SQLite + FTS5(레키시컬) + 선택적 sqlite-vec(가능 시). 벡터 미사용 모드에서도 하이브리드 degrade 허용.
- 프로덕션 권장: PostgreSQL 15+ + pgvector 0.6+ (HNSW 인덱스). 설정 권장값: cosine, m=16, ef_construction=128, ef_search=40, lists는 데이터 크기에 맞게 튜닝. 임베딩 차원: 1536(OpenAI text-embedding-3-small) 기본.
- 대안: Qdrant(로컬/도커) 1.8+ HNSW. 운영팀 성향/인프라에 따라 선택.

### 임베딩/번역 설정(제안)
- `MYCODER_EMBEDDING_PROVIDER`/`MYCODER_EMBEDDING_MODEL`로 코드 전용 임베딩 교체.
- `MYCODER_TRANSLATE_KO_EN`/`MYCODER_TRANSLATOR_MODEL`로 번역 폴백 경로 활성화.

## 4. 안정성/회복력
- 컨텍스트 타임아웃, 취소 전파, 재시도(지터), 서킷브레이커.
- 스트리밍(SSE), 백프레셔(배치/큐), 임베딩 캐시.

## 5. 보안
- 로컬 바운더리 기본, 키 스코프/적용 범위 최소화, 로그 마스킹.
- 외부 호출 옵트인, 도메인 허용목록, TTL/신뢰점수.

### 파일/터미널 권한 모델
- 기본 루트: 프로젝트 루트 내부로 파일 접근 제한(상대경로 정규화, 경로 탈출 방지). 외부 경로 접근은 명시적 플래그 필요(`--allow-outside-root`).
- 파괴적 동작: 삭제/대량 변경은 드라이런·미리보기·확정 단계 필요. CLI `--yes`로 무인 모드 허용 가능.
- 터미널 실행: 시간 제한/메모리 제한/출력 크기 제한. 명령 허용목록/차단목록 구성 가능. 입출력 스트리밍 및 구조화 로그 수집.

## 6. 관측성
- Zerolog 구조화 로그, 트레이스ID, Prometheus 지표(latency/hitrate/error).

## 7. 메모리(대화 이력) 계층
- 단기 메모리: 최근 메시지 슬라이딩 윈도우(토큰 예산 기반).
- 중기 요약: 주제별/러닝세션별 맵-리듀스 요약 버퍼, 중요 스니펫/결정/근거 파일 경로 유지.
- 장기 지식화: 검증된 설계/패턴/알고리즘은 Knowledge Store에 문서화하여 RAG로 재활용(인덱싱/임베딩).
- 세맨틱 캐시: 질의/응답 임베딩 기반 근사 캐시(히트 시 비용/지연 감소).
