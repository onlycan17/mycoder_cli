# 테스트/CI/후크 정책

## 게이트(차단 규칙)
- `make fmt` : `gofmt -s -w` 적용(자동 정리).
- `make fmt-check` : 포맷 미준수 파일이 있으면 실패.
- `make lint` : `go vet` 기반의 기본 정적 분석.
- `make test` : 단위/계약/소규모 e2e.

## 스모크 테스트(선택)
- 로컬 동작 확인: `make smoke`
  - PATH에 설치된 `mycoder` 확인(`which`/`version`) 후 서버를 백그라운드로 기동하고 `/healthz`까지 확인.
  - 임의 포트: `make smoke PORT=8090`
  - 설치 전이라면: `make install` 후 실행(기본 `$HOME/.mycoder/bin`)

## 프리커밋
- 커밋 직전에 `make fmt && make fmt-check && make test && make lint`를 실행하고, 하나라도 실패하면 커밋을 차단.
- 설치 방법:
  1) `make hook-install`
  2) 정상 설치 시 `.git/hooks/pre-commit`가 생성되고 실행 권한이 부여됨.
  3) 포맷팅으로 변경이 발생하면 훅이 자동으로 스테이징하여 일관성을 유지함.

## 테스트 전략
- 단위: 청커/리트리버/플래너/프롬프트 컴포저 테이블 기반 테스트.
- 계약: `VectorStore`/`Retriever`/`LLMProvider` 인터페이스 공통 케이스.
- 골든: 프롬프트/샘플 응답 스냅샷(완화 매칭), 회귀 방지.
- e2e: 소형 샘플 리포에서 `index → ask/edit → hooks` 연동 확인.
 - 보안/권한: 파일 경계(루트 밖 접근 차단)와 파괴적 동작 확인 흐름 테스트.
 - 쉘 실행: 시간 제한/출력 제한/허용·차단 목록 동작 테스트, 로그 구조 검증.
 - 메모리: 요약 정확성/TTL/삭제 정책, 세맨틱 캐시 히트율.

## 관측/로그
- 구조화 로그 수집, 실패 시 로그/디프/결과 요약 아티팩트 업로드.
 - 실행 로그(ExecutionLog) 아카이빙 및 재현 스크립트 생성.
