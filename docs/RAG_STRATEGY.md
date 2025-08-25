# RAG 임베딩 전략(초기 적용)

목표: 코드와 문서의 성격에 맞는 임베딩(설명: 텍스트를 숫자 벡터로 바꾸는 과정) 모델·프로바이더(설명: 임베딩을 생성하는 서비스)를 선택해 검색 품질을 높입니다.

핵심 아이디어
- 코드 파일 확장자 기반으로 코드 전용 임베딩 모델 사용
- 문서(README, md 등)는 일반 텍스트 임베딩 모델 사용
- 설정은 전부 환경변수로 관리하여 배포 환경에서 튜닝

환경변수
- `MYCODER_EMBEDDING_MODEL`: 기본 텍스트 모델(기본: `text-embedding-3-small`)
- `MYCODER_EMBEDDING_PROVIDER`: 기본 프로바이더(기본: `openai`)
- `MYCODER_EMBEDDING_MODEL_CODE`: 코드 전용 모델(없으면 기본 텍스트 모델 사용)
- `MYCODER_EMBEDDING_PROVIDER_CODE`: 코드 전용 프로바이더(없으면 기본 프로바이더 사용)
- `MYCODER_EMBEDDING_CODE_EXTS`: 코드 확장자 목록(콤마, 예: `go,ts,js,py`), 미설정 시 내장 기본 목록 사용
 - `MYCODER_EMBED_TRANSLATE_FALLBACK`: 한국어 감지 시 영어로 번역 후 임베딩(1=활성화)
 - `MYCODER_EMBED_TRANSLATE_TIMEOUT_MS`: 번역 타임아웃(ms), 기본 1200ms

동작 방식
- 임베딩 파이프라인은 파일 경로의 확장자를 보고 코드/문서를 구분하여 모델·프로바이더를 선택합니다.
- 서로 다른 모델은 배치(설명: 한번에 묶어서 처리) 단위로 그룹화하여 각각 별도로 호출합니다.
 - 번역 폴백이 켜진 경우, 텍스트에 한글(설명: 한글 유니코드 범위)이 포함되면 영어로 번역한 후 임베딩합니다. 번역 실패 시 원문으로 진행합니다.

추가 계획(후속)
- 다국어 문서: 언어 감지 후 한국어→영어 번역 폴백(설정/타임아웃/검증 포함)
- 코드 스니펫 청킹 개선: 토큰 기준 길이와 오버랩 조정(10~15%)
- 리더보드/가중치 튜닝: 하이브리드 검색 α 재튜닝, k@5/10/MRR 측정

청킹(Chunking) 설정
- `MYCODER_CHUNK_MAX_TOKENS`: 최대 토큰 수(기본 400)
- `MYCODER_CHUNK_OVERLAP_RATIO`: 청크 간 오버랩 비율(기본 0.10, 0~0.5)
- 코드 경계 우선: 언어별 함수/클래스 시그니처 기준으로 큰 블록을 만든 뒤 토큰 윈도우 적용

하이브리드 α 튜닝/리더보드
- 런타임 가중치: `MYCODER_HYBRID_ALPHA`(기본 0.5)
- 자동 평가: `go test ./internal/rag/retriever -run TestHybridAlphaLeaderboard -v` 실행 시 α grid(예: 0.0, 0.5, 1.0)에 대해 k@5/k@10/MRR 로깅
- 용어(k@K: 상위 K개 내 정답 포함 비율, MRR: 최초 정답 순위의 역수 평균)
