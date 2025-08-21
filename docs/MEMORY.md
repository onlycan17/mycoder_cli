# 대화 메모리 전략

## 목표
- 비용/지연을 관리하면서 맥락 유지 극대화.
- 중요한 결정/명령/근거를 잃지 않도록 구조화된 축약.

## 계층 구조
1) 단기(Short-term)
- 최근 메시지 슬라이딩 윈도우(토큰 예산 기반). 시스템/규칙 메시지 우선 포함.

2) 중기(Mid-term)
- 세션/주제별 맵-리듀스 요약: 핵심 결정, 명령, 관련 파일/라인 인용 보존.
- 재압축 정책: 요약 길이 초과 시 메타(근거 경로, 해시)만 남기고 텍스트 축약.

3) 장기(Long-term)
- 검증된 설계/패턴/알고리즘은 Knowledge Store에 문서로 영속화(RAG 재사용).
- 세맨틱 캐시(응답/패치): 동일/유사 질의에 대한 빠른 재사용.

## 보존/삭제 정책
- TTL: 비활성 세션 X일 후 원문 삭제, 요약만 유지.
- Recency-biased reservoir: 오래되되 자주 참조된 대화는 더 오래 보존.
- Pinned: 사용자가 고정한 세션/메시지는 삭제 제외.

## 검색/주입 전략
- 현재 쿼리 임베딩으로 과거 메시지/요약/지식 문서에서 상위 K 검색.
- 리랭크 후 컨텍스트 예산 내로 압축 삽입(인용 유지).

## 데이터 모델(추가)
- Conversation(id, projectID, title, createdAt, updatedAt, pinned?)
- ConversationMessage(id, convID, role, content, tokenCount, createdAt)
- ConversationSummary(id, convID, version, text, tokenCount, updatedAt)
- SemanticCache(id, projectID, queryHash, embeddingRef, answerRef, createdAt, hitCount)

## 테스트
- 요약 정확성(결정/근거 보존) 골든 테스트, TTL/삭제 시뮬레이션, 캐시 히트레이트 측정.

