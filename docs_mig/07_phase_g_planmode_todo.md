# Phase G — Plan Mode + 사용자 hook 핫리로드 + CLI 서브커맨드

## 1. 목표

마지막 phase. 사용자 친화 layer 를 완성한다.

1. **Plan Mode** — 실행 전 read-only 검토 → 승인 → 실행. claude_cli 동등.
2. **~/.infractl/hooks.yaml 핫리로드** — 재시작 없이 정책 갱신 (fsnotify 기반).
3. **CLI 서브커맨드** — `infractl hooks {test,list,validate}`, `infractl plan {enter,exit}`.
4. **TodoWrite 통합** — 다단계 작업 시 강제 (system prompt + hook 으로 mutation 도구 호출 시 todo 존재 검증).

종료 시점에 사용자가 체감할 변화:
- 위험 작업 전 Plan Mode 진입 → "이 명령들 실행할 거예요" 확인 → OK 후 진행.
- `~/.infractl/hooks.yaml` 수정 → 즉시 반영 (재시작 X).
- `infractl hooks test --command 'rm -rf /tmp'` 으로 hook 동작 미리 검증.
- 설치 같은 다단계 작업 시 TodoWrite 강제 → 단계 누락 방지.

---

## 2. claude_cli 참조 소스

| 영역 | claude_cli 경로 | 핵심 심볼 |
|---|---|---|
| Plan Mode 진입/종료 | `claude_cli/src/...` (plan mode 관련 — system prompt + tool 제한) | read-only mode 토글, 승인 gate |
| AgentTool runAgent | `claude_cli/src/tools/AgentTool/runAgent.ts` | mini-agent fork 패턴 (Plan Mode 의 검토 agent 가 활용 가능) |
| RemoteTriggerTool | `claude_cli/src/tools/RemoteTriggerTool/RemoteTriggerTool.ts` | 외부 트리거 처리 (참고용 — hook CLI 와 유사 패턴) |
| TodoWrite | `claude_cli/src/tools/TodoWriteTool/*` (해당 시) | todo 상태 관리, 강제 검증 |
| Hook CLI (참고 외부) | claude_cli 자체 hook test 명령은 제한적 — 우리는 자체 설계 |

→ 본 phase 시작 전 위 파일 읽고, **claude_cli 가 부족한 부분은 우리가 설계** (CLI 서브커맨드, fsnotify hot-reload).

---

## 3. 선행 조건

- [ ] Phase A 의 hook 시스템 + Phase D 의 hook 이관 완료
- [ ] Phase A 의 `internal/agent/todo` 골격 존재
- [ ] Phase B/C/D/E/F 안정 (회귀 0)
- [ ] §9 Phase F 종료 시 사용자 질문 답변 완료 (특히 Plan Mode 단축키)

---

## 4. 신설 / 수정 / 제거 파일

### 신설

```
internal/agent/planmode/
├── mode.go                 ← Plan Mode 상태 (Off / Active / PendingApproval)
├── transition.go           ← 진입/종료 transition (단축키 + 명시 명령)
├── readonly_filter.go      ← Plan Mode 동안 mutation 도구 차단
└── approval.go             ← 사용자 승인 UI 트리거

internal/hooks/
├── watcher.go              ← fsnotify 기반 hooks.yaml 핫리로드
└── reloader.go             ← yaml 파싱 → 정책 교체 (락 보호)

internal/agent/todo/
├── enforcer.go             ← mutation 도구 호출 시 todo 존재 검증 (hook PreToolUse 활용)
└── prompt_injector.go      ← 다단계 작업 감지 시 system prompt 에 "TodoWrite 사용" 주입

cmd/infractl/
├── hooks.go                ← `infractl hooks {test, list, validate, reload}` 서브커맨드
└── plan.go                 ← `infractl plan {enter, exit, status}` 서브커맨드
```

### 수정

```
internal/agent/query/engine.go (Phase B)
  ← Plan Mode 동안 readonly_filter.Apply() 호출

internal/tools/registry.go
  ← mutation 도구 분류 (write, exec, modify, ...)
  ← Plan Mode 가 read-only 만 허용

internal/tools/todo_write.go (Phase A)
  ← enforcer 가 호출, 상태 영속화

cmd/infractl/main.go
  ← hooks/plan 서브커맨드 wiring

internal/hooks/loader.go (Phase A)
  ← watcher 와 통합
```

### 제거 (Phase G 종료 후)

- 임시 hook reload 매커니즘 (있다면)
- 단계 보장 없는 multi-step 도구 호출 코드

---

## 5. 소단계 작업

### G.1  Plan Mode 상태 + transition
- 작업:
  - `planmode/mode.go`:
    - `type Mode int { Off, Active, PendingApproval }`
    - `Get(ctx) Mode`, `Set(ctx, mode) error` (세션 단위 저장)
  - `planmode/transition.go`:
    - 단축키 (사용자 질문 답변에 따라 — claude_cli 동일 Shift+Tab x2 또는 다른 키)
    - CLI: `infractl plan enter / exit`
    - 진입 시: read-only filter 활성화, 시스템 프롬프트에 "Plan Mode 안내" 주입
- 단위 테스트: 상태 전환, 진입/종료 시 sysprompt 변화

### G.2  read-only filter
- 작업:
  - `planmode/readonly_filter.go`:
    - tool registry 의 mutation 분류 활용
    - mutation 도구 호출 차단 → "Plan Mode 입니다, exit 후 다시" 메시지
    - 단, `probe_*`, `read_*`, `list_*` 등 read-only 는 통과
  - `query/engine.go` 가 매 도구 호출 전 `readonly_filter.Allow(tool)` 체크
- 단위 테스트: read tool 통과 / write tool 차단

### G.3  approval gate
- 작업:
  - `planmode/approval.go`:
    - Plan Mode 종료 명령 (`infractl plan exit`) 시:
      - 그동안 LLM 이 "실행하려 했던" 명령 목록 표시
      - 사용자 승인/거부 (TUI prompt 또는 CLI flag `--approve`)
    - 승인 시 → 명령들 실제 실행 (hook 정상 흐름)
    - 거부 시 → 모두 폐기
- 단위 테스트: 승인 / 거부 케이스

### G.4  hooks.yaml fsnotify 핫리로드
- 작업:
  - `go get github.com/fsnotify/fsnotify`
  - `hooks/watcher.go`:
    - `Watch(ctx, path) error` — 파일 변경 시 reloader.Reload 호출
  - `hooks/reloader.go`:
    - 새 yaml 파싱 → 검증 (yaml schema, hook 명령 존재 확인)
    - 검증 통과 시 정책 atomic 교체 (RWMutex 보호)
    - 실패 시 기존 정책 유지 + slog WARN
- 단위 테스트: yaml 변경 → 새 정책 적용 / 잘못된 yaml → 기존 유지

### G.5  CLI: infractl hooks
- 작업:
  - `cmd/infractl/hooks.go`:
    - `hooks list` — 현재 등록된 hook 목록 (matcher, backend, 위치)
    - `hooks test --event PreToolUse --tool Bash --input '{"command":"rm -rf /"}'` — hook 실행 시뮬레이션
    - `hooks validate ~/.infractl/hooks.yaml` — yaml 유효성 검증
    - `hooks reload` — 강제 재로드 (watcher 외)
- 단위 테스트: 각 서브커맨드 happy path / 잘못된 입력 에러 메시지

### G.6  CLI: infractl plan
- 작업:
  - `cmd/infractl/plan.go`:
    - `plan enter` — Plan Mode 진입
    - `plan exit [--approve | --reject]` — 종료 + 승인/거부
    - `plan status` — 현재 상태 + 대기 중 명령 목록
- 단위 테스트: 각 서브커맨드 흐름

### G.7  TodoWrite enforcer + prompt injector
- 작업:
  - `agent/todo/prompt_injector.go`:
    - 사용자 입력에 다단계 신호어 (설치, 배포, 마이그레이션, ...) 감지
    - system prompt 에 "TodoWrite 사용 강제" 한 줄 추가
  - `agent/todo/enforcer.go`:
    - hook PreToolUse 로 등록 (또는 query.Engine 안 게이트)
    - mutation 도구 호출 시 todo list 가 비어있으면 거부
    - "먼저 TodoWrite 로 단계 작성하세요" 메시지
- 단위 테스트: 신호어 감지 / 거부 메시지 / todo 있을 시 통과

### G.8  통합 + E2E
- 작업:
  - 모든 컴포넌트 wiring
  - cmd/infractl 부트스트랩에 watcher 시작
  - Plan Mode 진입 단축키를 TUI 에 바인딩
- 단위 테스트: 통합 happy path

---

## 6. CLAUDE.md 규칙 준수 포인트

- [ ] 각 파일 300줄 이내 (mode/transition/filter/approval 분리)
- [ ] file header DocBlock
- [ ] 모든 함수 첫 인자 `ctx context.Context`
- [ ] fsnotify watcher 종료 시 ctx cancel 로 깔끔히 정리
- [ ] hooks.yaml 권한 체크 (group/world writable 거부) — Phase D 의 규칙 재적용
- [ ] CLI 서브커맨드 출력에 크리덴셜 노출 금지 (hook input 에 비밀 가능)
- [ ] slog: plan mode transition, hook reload, todo enforce 구조화 로그

---

## 7. 검증 방법

### 단위 테스트
- planmode mode/transition/filter/approval
- hooks watcher (fsnotify mock) / reloader (잘못된 yaml)
- todo enforcer / prompt injector
- CLI 서브커맨드

### 통합 테스트
- `//go:build integration`
- 실제 fsnotify 로 hooks.yaml 변경 → 정책 반영
- 실제 LLM 으로 Plan Mode 진입 → mutation 차단 → exit + 승인 → 실행

### E2E 시나리오
- 시나리오 1: 사용자 "/etc/nginx 변경 후 systemctl restart" 요청 → Plan Mode 진입 → 명령 목록 표시 → 승인 → 실행
- 시나리오 2: Plan Mode 안에서 LLM 이 `rm` 시도 → 차단 + 안내 메시지
- 시나리오 3: `~/.infractl/hooks.yaml` 에 새 PreToolUse 추가 → 즉시 반영 → 다음 명령에 적용
- 시나리오 4: `infractl hooks test --event PreToolUse --tool Bash --input '{"command":"rm -rf /"}'` → 차단 결과 + 어떤 hook 매칭됐는지
- 시나리오 5: 사용자 "MySQL 5.7 → 8.0 마이그레이션" → 신호어 감지 → TodoWrite 강제 → 단계별 진행
- 시나리오 6: TodoWrite 비어있는데 mutation 시도 → 거부 메시지
- 시나리오 7: `infractl hooks validate /tmp/bad.yaml` → 유효성 에러 메시지

### 빌드
- `go build -o bin/infractl.exe ./cmd/infractl/`
- 회귀 0

---

## 8. 종료 조건

- [ ] §7 모든 검증 통과
- [ ] hooks.yaml 핫리로드 E2E 통과
- [ ] Plan Mode E2E 통과 (TUI + CLI 양쪽)
- [ ] TodoWrite enforcer 동작 검증
- [ ] `docs/design/todo-planmode.md` 작성/갱신
- [ ] `docs/design/hooks.md` CLI/핫리로드 섹션 추가
- [ ] `docs/infractl-architecture.md` Plan Mode + CLI 섹션 추가
- [ ] `docs_mig/README.md` 전 phase 완료 상태 update
- [ ] **재설계 완료 보고** — 사용자에게 전체 결과 (성능, 토큰 절감, 위험 차단 정확도) 보고

---

## 9. 다음 단계 (마이그레이션 종료 후)

```
[ ] R1. 운영 모니터링 — Phase 8 (별도 — 메모리 규칙: Phase 7/8 맨 마지막)
[ ] R2. Daemon + Web UI — Phase 7 (별도)
[ ] R3. 사용자 사이트 별 hook bundle 공유 (org-wide hooks)
[ ] R4. 추가 backend 검토 (gRPC, Kafka, ...)
```

---

## 끝.
