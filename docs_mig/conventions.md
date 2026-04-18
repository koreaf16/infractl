# conventions.md — claude_cli 흡수 마이그레이션 작업 규칙

본 문서는 `docs_mig/` 의 모든 phase 작업에 공통 적용되는 규칙이다.  
**모든 phase 문서는 이 규칙을 전제로 작성/실행된다.**

---

## 1. claude_cli 원본 참조 의무 (★ 필수)

### 1.1 작업 전

각 phase의 소단계마다 **반드시 `claude_cli/src/`의 해당 원본을 Read/Grep으로 직접 확인**하고 시작한다.

```
phase 문서 → "claude_cli 참조 소스" 섹션 → 명시된 파일/심볼/라인을 Read
                                       ↓
                                포팅 대상 패턴 정확히 파악
                                       ↓
                                Go 코드 작성
```

추측·기억으로 작성 금지. 매번 실제 소스를 본다.

### 1.2 코드 주석 의무

포팅한 모든 함수/타입/메서드에 출처를 코드 주석으로 남긴다.

```go
// Ported from: claude_cli/src/services/tools/StreamingToolExecutor.ts:40-519
// 핵심 패턴: partition by isConcurrencySafe → read-only는 병렬, mutation은 직렬,
//            완료된 결과는 즉시 채널로 yield, sibling 실패 시 abort 전파.
type StreamingExecutor struct { ... }
```

- 출처 없는 포팅은 PR 리뷰에서 reject.
- 동일 패턴이 같은 파일에서 반복되면 파일 상단에 한 번만 명시 후, 함수 단위는 짧게 cross-reference (`see top of file`).

### 1.3 claude_cli 수정 금지 (CLAUDE.md 기존 규칙 재확인)

- `claude_cli/src/`는 UI/패턴 참고용. **절대 수정하지 않는다.**
- 별도 언급이 없는 한 단순 조회도 하지 않는다 — 단, 본 마이그레이션 작업 중에는 phase 문서가 명시한 파일을 "조회 허용 대상"으로 간주한다.

---

## 2. TypeScript → Go 포팅 매핑 규칙

| TypeScript 패턴 | Go 매핑 | 비고 |
|---|---|---|
| `AsyncGenerator<T>` / `yield` | `chan T` + goroutine + `defer close(ch)` | sender가 close 책임 |
| `Promise.all([a, b, c])` | `golang.org/x/sync/errgroup.Group` | 첫 에러 시 ctx 취소 전파 |
| `AbortController.signal` | `context.Context` (`ctx.Done()`) | 모든 I/O는 ctx 첫 인자 |
| 동적 `import()` (lazy) | `sync.Once` + 패키지 init 지연 패턴 | 초기 비용 큰 모듈 |
| `try/catch + rethrow` | `if err != nil { return fmt.Errorf("...: %w", err) }` | wrap 필수 |
| `interface I { foo(): T }` | `type I interface { Foo() T }` | 소비자 패키지에 정의 |
| Discriminated union (`type:'a'\|'b'`) | 인터페이스 + 구체 타입별 `Type()` 메서드 또는 sealed enum struct | switch type assertion |
| `Map<K,V>` | `map[K]V` 또는 `sync.Map` (concurrent) | 동시성 여부에 따라 |
| `setTimeout(fn, ms)` | `time.AfterFunc(d, fn)` | cancel 가능 |
| `setInterval` | `time.Ticker` + goroutine | `defer ticker.Stop()` |
| `EventEmitter.on/emit` | `chan Event` (1-to-1) 또는 fan-out 헬퍼 | EventEmitter 직역 금지 |
| `JSON.parse/stringify` | `encoding/json` (`Unmarshal/Marshal`) | 스키마 struct 정의 |
| TypeScript 옵셔널 (`x?: T`) | `*T` 포인터 또는 별도 `bool` flag | nil 체크 의무 |
| Tagged template / template literal | `fmt.Sprintf` 또는 `strings.Builder` | 다중 라인은 raw string |

### 2.1 Streaming Generator 표준 시그니처

```go
// claude_cli의 AsyncGenerator<QueryEvent> 대응
func (e *Engine) Run(ctx context.Context, in QueryParams) (<-chan QueryEvent, error) {
    out := make(chan QueryEvent, 8)
    go func() {
        defer close(out)
        // ...
        select {
        case out <- ev:
        case <-ctx.Done():
            return
        }
    }()
    return out, nil
}
```

소비자는 `for ev := range out { ... }`. 에러 이벤트는 채널 안에 별도 타입으로.

---

## 3. CLAUDE.md 규칙 재확인 (포팅 시 자주 어기는 것들)

phase 작업 중 **반드시 점검**:

- [ ] 파일당 **300라인 제한** — claude_cli의 1500줄짜리 query.ts를 그대로 옮기지 말 것. 책임별로 분할.
- [ ] **File header DocBlock** 모든 신규 .go 파일 최상단에:
  ```go
  // Package <pkg>
  // File: <name>.go
  // Description: <한 줄 요약>
  // Responsibility: <단일 책임>
  ```
- [ ] **에러 wrap**: `fmt.Errorf("ssh dial %s: %w", addr, err)` — `_ =` 절대 금지.
- [ ] **context.Context 첫 인자**: 모든 I/O 함수.
- [ ] **인터페이스는 소비자 패키지에 정의** — 구체 타입 직접 참조 금지.
- [ ] **slog 사용** — `fmt.Println`/`log.Printf` 금지, 크리덴셜 로그 금지.
- [ ] **빌드 출력**: `go build -o bin/infractl.exe ./cmd/infractl/`
- [ ] **명령 인자 분리**: `exec.Command("ls", userInput)` — 쉘 문자열 결합 금지.
- [ ] **DB 파라미터 바인딩**: SQL 문자열 포맷팅 금지.
- [ ] **mock 금지** SSH/DB 통합 테스트.

---

## 4. Phase 진행 절차 (모든 phase 공통)

```
[phase 진입 전]
  1. 사용자에게 진입 확인 질문 (해당 phase 문서의 "결정 필요" 답변 받기)
  2. 이전 phase의 "종료 조건" 모두 통과 확인
  3. 해당 phase 문서의 "claude_cli 참조 소스" 명시된 파일 모두 Read

[phase 진행 중]
  4. 소단계 단위로 작업 (1.1, 1.2, ...). 각 소단계 = 1 PR.
  5. 코드 작성 시 위 규칙 (claude_cli 출처 주석, CLAUDE.md 규칙) 모두 준수.
  6. 각 소단계 종료 시:
     - go build 통과
     - 해당 소단계의 단위 테스트 추가 + 통과
     - 골든/E2E 회귀로 교체 안정성 검증

[phase 종료]
  7. "검증 방법" 섹션의 모든 시나리오 통과
  8. "종료 조건" 모두 충족 확인
  9. docs/infractl-architecture.md 갱신 (해당 phase의 변경분 반영)
  10. docs/design/<해당 영역>.md 갱신 (필요시)
  11. docs_mig/README.md 의 진행 현황 표 업데이트
  12. 사용자에게 종료 보고 + 다음 phase 진입 질문
```

---

## 5. 문서 위치 규칙

| 문서 | 위치 | 작성/갱신 시점 |
|---|---|---|
| 페이즈별 마이그레이션 계획 | `docs_mig/0X_phase_*.md` | 본 세션에 일괄 작성 (이후 변경 시만 수정) |
| 전체 비전 | `docs/design/redesign-overview.md` | 본 세션 작성, 큰 방향 변경 시 갱신 |
| 영역별 상세 설계 | `docs/design/<area>.md` | 해당 phase **시작 직전** 별도 세션에서 작성 |
| 아키텍처 메인 | `docs/infractl-architecture.md` | 각 phase **종료 시** 변경분 반영 |
| 페이즈 인덱스 | `docs_mig/README.md` | 각 phase 진행 상태 변경 시 |

---

## 6. 대규모 교체 검증 규칙

대규모 교체(특히 Phase B의 query engine)는 반드시 검증으로 보호한다:

- 교체 대상의 골든 시나리오를 먼저 정의한다.
- 기존 사용자 흐름 E2E를 실행해 출력/도구 결과/종료 사유 회귀를 확인한다.
- query engine 교체는 Phase B에서 직접 적용하고 기존 루프를 제거한다.
- 제거 대상 파일이 있으면 phase 문서의 제거 섹션에 명시하고 종료 보고서에 포함한다.

---

## 7. 작업 시 절대 하지 말 것

- ❌ claude_cli 소스를 보지 않고 "기억으로" 포팅
- ❌ phase 순서 건너뛰기 (선행 의존성 무시)
- ❌ 한 PR에 여러 phase 섞기
- ❌ 골든/E2E 회귀 없이 query engine 직접 교체
- ❌ preflight/* 코드를 Phase D 동등성 검증 전에 제거
- ❌ Plan Mode 진입 없이 위험·대량 작업 직접 mutation
- ❌ TodoWrite 없이 다단계 작업(설치 등) 진행
- ❌ Hook 작성 후 `infractl hooks test` 검증 생략

---

## 끝.
