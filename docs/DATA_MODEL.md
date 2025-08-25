# 데이터 모델 / ERD

## 엔티티
- Project(id, name, rootPath, createdAt)
- Document(id, projectID, sourceType(code|doc|web), path|url, sha|etag, lang, title, createdAt, updatedAt)
- Chunk(id, docID, ordinal, text, tokenCount, symbolRefs, createdAt)
- Embedding(id, chunkID, provider, dim, vectorRef, createdAt)
- TermIndex(docID, term, tfidf)
- Message(id, projectID, role, content, createdAt)
- Run(id, projectID, type(chat|edit|index|hooks), status, startedAt, finishedAt, metrics, logsRef)
- Patch(id, runID, path, diff, applied, createdAt)
- WebSource(id, url, domain, fetchedAt, ttl, trustScore)
- Symbol(id, projectID, name, kind, file, rangeStart, rangeEnd, refsCount)

### 대화/메모리 추가
- Conversation(id, projectID, title, createdAt, updatedAt, pinned)
- ConversationMessage(id, convID, role, content, tokenCount, createdAt)
- ConversationSummary(id, convID, version, text, tokenCount, updatedAt)
- SemanticCache(id, projectID, queryHash, embeddingRef, answerRef, createdAt, hitCount)
- ExecutionLog(id, runID, kind(shell|fs|hook|mcp), payloadRef, startedAt, finishedAt, exitCode)

### 지식(검증/승격) 추가
- Knowledge(id, projectID, sourceType(code|doc|web), pathOrURL, title, text,
  trustScore, pinned, commitSHA, files(csv), symbols(csv), tags(csv),
  createdAt, verifiedAt)
- Review/Feedback/Blocklist(후속 단계)

## 인덱스/키
- PK: 각 id, FK: projectID/docID/chunkID 관계 유지.
- 색인: Document(path,projectID), Chunk(docID,ordinal), TermIndex(term), Symbol(name,kind), Run(startedAt).
- 벡터 인덱스: 외부 VectorStore(Qdrant/pgvector/sqlite-vec) 관리.

## SQLite 테이블(요약)
- embeddings(id, project_id, doc_id, chunk_id, provider, model, dim, vector JSON, created_at)
- patches(id, project_id, path, hunks JSON, applied, created_at, applied_at)
- symbols(id, project_id, path, lang, name, kind, start_line, end_line, signature, created_at)

## 벡터 스토어 권장 스펙
- 로컬/개발: SQLite+FTS5(필수), sqlite-vec(선택). 벡터 미사용 시에도 레키시컬 검색으로 동작.
- 프로덕션 권장: PostgreSQL 15+ + pgvector 0.6+ (HNSW). cosine, m=16, ef_construction=128, ef_search=40, lists는 데이터 크기별 튜닝. 임베딩 차원 1536.
- 대안: Qdrant 1.8+ (도커), HNSW 파라미터는 유사 수준으로 설정.

### 검색 스코프(모델 분리)
- 동일 차원의 서로 다른 임베딩 모델이 혼합되면 검색 품질이 저하될 수 있음.
- 검색 시 스코프를 `project_id + dim + model`로 제한하는 것을 권장(로컬 SQLite 구현부터 적용 가능).
- 저장 필드: `embeddings(provider, model, dim)`를 사용하여 모델 단위로 색인을 분리.

## 무결성/정책
- Document.sha/etag로 변경 감지 → 증분 인덱싱.
- WebSource TTL 만료 시 재수집.
- Run/LogsRef로 실행 이력/재현성 보장.
 - Conversation TTL/요약 정책으로 원문 축소/삭제.
