# checklist_per_phase.md — 페이즈 공통 체크리스트 / 표준 섹션 템플릿

본 문서는 `docs_mig/0X_phase_*.md` 의 **표준 9개 섹션 템플릿**과,  
phase 진입 전·진행 중·종료 시 점검할 **체크리스트**를 정의한다.

---

## A. 페이즈 문서 표준 9개 섹션

각 phase 문서는 아래 순서로 9개 섹션을 모두 포함한다.

### 1. 목표 (한 문단)
- 이 phase가 무엇을 달성하는가
- 왜 지금 이걸 하는가 (선행 phase와의 관계)
- 종료 시점에 사용자가 체감할 변화

### 2. claude_cli 참조 소스 (★ 필수)
포팅 대상 원본 파일·심볼·라인을 표로 명시.

```
| 영역 | claude_cli 경로 | 핵심 심볼 / 라인 |
|---|---|---|
| ... | claude_cli/src/... | ... |
```

`conventions.md` §1.1 — 작업 시 이 파일들을 반드시 Read.

### 3. 선행 조건
이전 phase의 어떤 종료 조건이 충족되어야 진입 가능한가.

- [ ] Phase X.Y 종료 조건 모두 통과
- [ ] 회귀 테스트 통과
- [ ] 제거/교체 범위 정리 완료 (해당 시)

### 4. 신설 / 수정 / 제거 파일
```
신설:
  - internal/<pkg>/<file>.go     ← 책임
  - ...

수정:
  - internal/<pkg>/<file>.go     ← 어떻게
  - ...

제거 (있는 경우, 사용자 사전 승인 필수):
  - internal/<pkg>/<file>.go     ← 언제 (어느 소단계 종료 후)
```

### 5. 소단계 작업 (각 소단계 = 1 PR)
```
X.1 <소단계 제목>
    - claude_cli 참조: <파일:라인>
    - 작업 내용: ...
    - 산출물: <파일 목록>
    - 단위 테스트: ...

X.2 ...
```

### 6. CLAUDE.md 규칙 준수 포인트
이 phase에서 특히 어기기 쉬운 규칙 강조.

- [ ] 파일당 300라인 제한 (대규모 모듈 분할 명시)
- [ ] File header DocBlock 모든 신규 파일
- [ ] 에러 wrap 의무
- [ ] context.Context 첫 인자
- [ ] 인터페이스는 소비자 패키지에 정의
- [ ] slog 사용, 크리덴셜 로그 금지
- [ ] (해당 phase 특수 규칙)

### 7. 검증 방법
```
단위 테스트:
  - <패키지> 의 ...

통합 테스트:
  - //go:build integration
  - 실제 SSH/DB 연결 (mock 금지)

E2E 시나리오:
  - 시나리오 1: ...
  - 시나리오 2: ...

빌드:
  - go build -o bin/infractl.exe ./cmd/infractl/
  - 골든 시나리오 회귀 (기존 사용자 흐름 대비 출력/도구 결과/종료 사유 확인)
```

### 8. 종료 조건
"이게 모두 충족되어야 다음 phase 진입 가능"

- [ ] §7 검증 모두 통과
- [ ] 제거/교체 대상 정리 완료 (해당 시)
- [ ] docs/infractl-architecture.md 갱신
- [ ] docs/design/<area>.md 갱신
- [ ] docs_mig/README.md 진행 현황 업데이트
- [ ] (phase 특수 종료 조건)

### 9. 다음 phase 진입 전 사용자 질문 항목
다음 phase 시작 시 확인할 결정 사항을 미리 적어둔다.

```
[ ] Q1. ...
[ ] Q2. ...
[ ] Q3. ...
```

---

## B. Phase 진입 전 체크리스트 (사용자 + Claude)

```
□ 이전 phase 의 §8 종료 조건 모두 통과 확인
□ 이전 phase 종료 보고서 (텍스트) 확인
□ 본 phase 의 §9 사용자 질문 항목 답변 완료
□ 본 phase 의 §2 claude_cli 참조 소스 모두 Read 가능 (파일 존재 확인)
□ 본 phase 의 §4 영향 받는 파일 목록 검토 완료
□ 제거/교체 범위와 회귀 검증 전략 합의
□ 작업 단위 (PR 수, 예상 일정) 합의
□ "phase 시작 OK" 명시적 승인 받음
```

---

## C. Phase 진행 중 체크리스트 (소단계마다)

```
□ 해당 소단계의 claude_cli 원본 Read 완료
□ Go 파일에 출처 주석 작성 (Ported from: ...)
□ File header DocBlock 작성
□ 파일 300라인 이내 (초과 시 분할)
□ 에러 wrap, context 첫 인자, slog 사용 등 CLAUDE.md 규칙 준수
□ 단위 테스트 추가
□ go build 통과
□ 기존 회귀 테스트 통과
□ PR 단위 commit 메시지 (Conventional Commits)
```

---

## D. Phase 종료 체크리스트

```
□ §7 검증 시나리오 100% 통과
□ §8 종료 조건 모두 ✓
□ 제거/교체 대상 정리 완료 (해당 시)
□ docs/infractl-architecture.md 갱신
□ docs/design/<area>.md 갱신/신설
□ docs_mig/README.md 진행 현황 표 update
□ 사용자에게 종료 보고서 전달 (성과, 잔여 이슈, 다음 phase 진입 질문 포함)
```

---

## E. 종료 보고서 템플릿

```
# Phase <X> 종료 보고서

## 1. 달성한 것
- ...

## 2. 추가/수정/제거된 파일 목록
- (final diff 요약)

## 3. 검증 결과
- 단위 테스트: PASS (n개)
- 통합 테스트: PASS (n개)
- E2E 시나리오: 시나리오별 PASS/FAIL
- 빌드: PASS

## 4. 잔여 이슈 / 알려진 한계
- ...

## 5. 다음 phase 진입 전 질문
- Q1. ...
- Q2. ...

## 6. 통계
- 코드 라인 +X / -Y
- claude_cli 참조 파일 N개
- 작업 일수: ...
```

---

## 끝.
