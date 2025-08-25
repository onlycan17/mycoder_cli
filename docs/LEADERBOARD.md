# 리더보드 측정 방법

목표: 하이브리드 검색(BM25+KNN)의 α(설명: KNN 가중치)를 튜닝하고 k@5/10, MRR을 측정합니다.

두 가지 방식
- 합성 데이터 테스트: 이미 포함된 테스트로 α별 지표를 빠르게 확인
- 데이터셋 기반 실측: 문서+질문/정답셋(JSON)을 제공하여 실제 검색 파이프라인에 가깝게 평가

실행 방법
- 합성 리더보드: `go test ./internal/rag/retriever -run TestHybridAlphaLeaderboard -v`
- 데이터셋 리더보드:
  1) JSON 파일 준비(example):
  ```json
  {
    "documents": [
      {"path": "a.md", "text": "Install guide ..."},
      {"path": "b.go", "text": "package main\nfunc Run() {}"}
    ],
    "cases": [
      {"query": "how to install", "truth": ["a.md"]},
      {"query": "run function", "truth": ["b.go"]}
    ]
  }
  ```
  2) 환경변수 지정 후 테스트 실행:
  `MYCODER_EVAL_CASES=./path/to/cases.json go test ./internal/rag/retriever -run TestDatasetEvaluateIfProvided -v`

동작 원리
- BM25: 인메모리 스토어의 단순 검색(
  설명: 소문자 포함 여부·빈도 기반) 사용
- KNN: 테스트 전용 보우(BOW, 설명: 단어 빈도 기반) 임베딩과 메모리 벡터스토어로 코사인 유사도 검색
- 하이브리드: `score = bm25 + α * knn`, α는 `MYCODER_HYBRID_ALPHA`(기본 0.5)

주의
- 테스트 환경을 위한 경량 평가입니다. 프로덕션 환경과 정확히 동일하지 않을 수 있습니다.
- 실제 모델 임베딩/벡터스토어를 사용하려면 해당 어댑터로 바꿔서 동일 패턴의 테스트를 확장하면 됩니다.

