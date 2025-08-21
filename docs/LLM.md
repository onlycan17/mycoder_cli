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
- `MYCODER_OPENAI_BASE_URL=http://192.168.0.227:3620/v1`
- `MYCODER_OPENAI_API_KEY=` (빈값 허용)
- `MYCODER_EMBEDDING_MODEL=text-embedding-3-small` (로컬 임베딩 제공 시 해당 이름 사용)
- `MYCODER_CHAT_MODEL=gpt-3.5-turbo` (LM Studio에 로드된 모델 식별자로 대체)

#### 제약/권고
- 토큰화/컨텍스트 윈도우가 모델별로 상이 — 청크/프롬프트 토큰 예산을 모델별로 조정.
- 스트리밍: SSE 전송 버퍼 제한 — 긴 출력은 중간 플러시.
- 성능: CPU/GPU 자원과 회선에 의존 — 동시성 제한(서버 설정 또는 레이트리미트).

### OpenAI (클라우드) 연동(옵션)
- `MYCODER_OPENAI_BASE_URL=https://api.openai.com/v1`
- `MYCODER_OPENAI_API_KEY=sk-...`

### 공통 구성
- 타임아웃/재시도: 백오프+지터, 429/5xx 재시도, 사용자 중단 신호 처리
- 레이트 리미트: 토큰/요청 기반 슬라이딩 윈도우
- 로깅: 요청 메타(모델/토큰/소요)만, 프롬프트/응답 전문은 옵트인 마스킹
 - 최소 간격: `MYCODER_LLM_MIN_INTERVAL_MS`(클라이언트 측 요청 간 최소 간격, ms)

### 지원 엔드포인트(호환)
- `GET /v1/models`
- `POST /v1/chat/completions`
- `POST /v1/completions`
- `POST /v1/embeddings`

### 테스트 전략
- 모의 서버(mock): OpenAI/Anthropic 프로토콜 스텁으로 단위 테스트
- 선택적 통합 테스트: LM Studio 구동 시 스킵 불가한 테스트는 opt-in 태그로 분리
