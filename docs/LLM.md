## LLM 연동 명세 (로컬/클라우드)

### 개요
- 목표: 공급자 추상화로 클라우드(Anthropic/Claude, OpenAI 등)와 로컬(LM Studio 등) API를 동일한 UX로 사용.
- 프로토콜: SSE 스트리밍 지원, OpenAI 호환 엔드포인트 우선.

### 지원 대상(v1)
- LM Studio(로컬, OpenAI 호환) 우선 지원
- 옵션: OpenAI 호환 게이트웨이(베이스 URL 교체로 사용 가능)

### LM Studio (로컬) 연동
- 엔드포인트: 기본 `http://localhost:1234/v1`
- 프로토콜: OpenAI 호환(Completions/ChatCompletions/Embeddings)
- 인증: 보통 불필요(빈 키) — 필요 시 `MYCODER_OPENAI_API_KEY`에 더미값 허용

#### 설정(환경변수)
- 공통
  - `MYCODER_OPENAI_BASE_URL=http://192.168.0.227:3620/v1`
  - `MYCODER_OPENAI_API_KEY=` (빈값 허용)
- 채팅 모델(고정 정책)
  - `MYCODER_CHAT_MODEL=qwen3-coder-30b-a3b-instruct`
- 임베딩(권장)
  - 코드 전용 임베딩 사용 시: `MYCODER_EMBEDDING_PROVIDER=codexembed`, `MYCODER_EMBEDDING_MODEL=<codexembed-model-id>`
  - 일반 텍스트 임베딩: `MYCODER_EMBEDDING_MODEL=text-embedding-3-small`
- 번역 폴백(옵션)
  - `MYCODER_TRANSLATE_KO_EN=1`
  - `MYCODER_TRANSLATOR_MODEL=<translator-model-id>`
  - `MYCODER_TRANSLATE_TIMEOUT_MS=2500`

#### 제약/권고
- 토큰화/컨텍스트 윈도우가 모델별로 상이 — 청크/프롬프트 토큰 예산을 모델별로 조정.
- 스트리밍: SSE 전송 버퍼 제한 — 긴 출력은 중간 플러시.
- 성능: CPU/GPU 자원과 회선에 의존 — 동시성 제한(서버 설정 또는 레이트리미트).

### OpenAI (클라우드) 연동(옵션)
- `MYCODER_OPENAI_BASE_URL=https://api.openai.com/v1`
- `MYCODER_OPENAI_API_KEY=sk-...`

#### OpenAI 전환 가이드(보안 포함)
- 목적: 로컬(LM Studio 등)에서 클라우드(OpenAI)로 손쉽게 전환
- 단계:
  1) 환경변수 설정
     - `export MYCODER_OPENAI_BASE_URL=https://api.openai.com/v1`
     - `export MYCODER_OPENAI_API_KEY=sk-...` (필수)
  2) 연결 테스트
     - `mycoder models` — OpenAI `/v1/models` 응답 확인
  3) 사용 예
     - `mycoder chat --project <id> "이 파일 설명: internal/server/server.go"`
  4) 보안 권고
     - 키는 셸 히스토리/프로세스 리스트에 노출되지 않도록 `.env`나 비밀 관리 도구 사용
     - 저장소에 키를 커밋하지 않기(.gitignore 활용)
     - 프록시/회사 게이트웨이 사용 시 베이스 URL만 교체

### 공통 구성
- 타임아웃/재시도: 백오프+지터, 429/5xx 재시도, 사용자 중단 신호 처리
- 레이트 리미트: 토큰/요청 기반 슬라이딩 윈도우
- 로깅: 요청 메타(모델/토큰/소요)만, 프롬프트/응답 전문은 옵트인 마스킹
 - 최소 간격: `MYCODER_LLM_MIN_INTERVAL_MS`(클라이언트 측 요청 간 최소 간격, ms)
- 임베딩 폴백: 임베딩 모델/엔드포인트가 없거나 오류 시 서버가 자동으로 임베딩을 비활성화(레키시컬만 사용). 강제 비활성화: `MYCODER_DISABLE_EMBEDDINGS=1`.

### Qwen 계열 모델 최적화 가이드(요약)
- 프롬프트 스타일: 근거 우선, 인용 강제(`path:start-end`), 불확실 시 "모름". 한국어 답변을 기본으로 지시.
- 컨텍스트 예산: 모델 컨텍스트 윈도우에 맞춰 동적으로 조정(초기값은 RAG 컴포저의 바이트 예산 사용, 모델 스펙 확인 후 상향 가능).
- 다국어 질의: 기본은 한글 질의 직접 검색, 부족 시 KO→EN 번역 후 재검색 루틴 활성화.
- 임베딩: 코드 전용 임베딩 + 모델 스코프 분리(동일 차원 타 모델 혼류 방지). 차원 변경 시 재색인.

### 지원 엔드포인트(호환)
- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/completions`
- `POST /v1/embeddings`

### 테스트 전략
- 모의 서버(mock): OpenAI/Anthropic 프로토콜 스텁으로 단위 테스트
- 선택적 통합 테스트: LM Studio 구동 시 스킵 불가한 테스트는 opt-in 태그로 분리

### LM Studio 스모크 테스트(옵트인)
- 환경변수 설정:
  - `export MYCODER_LMSTUDIO_SMOKE=1`
  - `export MYCODER_OPENAI_BASE_URL=http://localhost:1234/v1`
  - `export MYCODER_OPENAI_API_KEY=` (필요 시)
- 실행: `make test`
  - 네트워크 가능 환경에서 `/v1/models`로 헬스 체크(기본 3초 타임아웃)
  - 미설정 시 자동 스킵
