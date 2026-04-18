# Phase A — 인프라 신설 (무손상)

## 1. 목표

claude_cli 흡수의 **모든 후속 phase가 의존할 골격**을 신설한다.  
이 phase 는 **기존 동작에 영향이 0** 이어야 한다 (코드 추가만, 호출 경로 변경 없음).

종료 시점에 사용자가 체감할 변화: **PostToolUse hook 으로 감사 로그/SIEM 전송이 가능**해진다.  
나머지 신규 패키지는 다음 phase에서 활성화된다.

---

## 2. claude_cli 참조 소스

| 영역 | claude_cli 경로 | 핵심 심볼 / 라인 |
|---|---|---|
| Hook 스키마 | `claude_cli/src/schemas/hooks.ts` | `HooksSettings`, `HookMatcher`, `HookCommand` (211–222) |
| Hook 실행 헬퍼 | `claude_cli/src/utils/hooks/` (디렉토리 전체) | `execShellCommand`, `execPromptHook`, `execHttpHook`, `execAgentHook` |
| Hook 설정 스냅샷 | `claude_cli/src/utils/hooks/hooksConfigSnapshot.ts` | `getHooksConfigFromSnapshot` (119–124), `captureHooksConfigSnapshot` |
| Hook 이벤트 emit | `claude_cli/src/utils/hooks/hookEvents.ts` | `registerHookEventHandler`, `emitHookStarted/Progress/Response` (56–150) |
| Context fetch (parallel) | `claude_cli/src/services/queryContext.ts` | `fetchSystemPromptParts` (44–74) |
| Context 함수 | `claude_cli/src/services/context.ts` | `getGitStatus` (36), `getClaudeMds` (170), `getUserContext` (155–189) |
| System prompt boundary | `claude_cli/src/utils/api.ts` | `splitSysPromptPrefix` (DYNAMIC_BOUNDARY) |
| Shell Provider 인터페이스 | `claude_cli/src/utils/shell/shellProvider.ts` | `SHELL_TYPES`, `ShellProvider` (1–34), `resolveDefaultShell` |
| Bash Provider | `claude_cli/src/utils/shell/bashProvider.ts` | `createBashShellProvider`, `buildExecCommand` (58–252) |
| PowerShell Provider | `claude_cli/src/utils/shell/powershellProvider.ts` | `createPowerShellProvider`, `encodePowerShellCommand` (27–124) |
| Agent (Task) tool | `claude_cli/src/tools/AgentTool/runAgent.ts` | `runAgent` (1–83) — TodoWrite 패턴 참고 (LLM이 작업 목록 생성하는 흐름) |

→ **소단계 시작 전 위 파일들 Read 필수** (`conventions.md` §1.1).

---

## 3. 선행 조건

- [ ] CLAUDE.md 규칙 재확인 (300줄, DocBlock, 에러 wrap, slog 등)
- [ ] `docs_mig/conventions.md` `checklist_per_phase.md` 숙지
- [ ] go module 상태 정상 (`go build ./...` 통과)

---

## 4. 신설 / 수정 / 제거 파일

### 신설

```
internal/hooks/
├── event.go              ← HookEvent 상수, HookInput/Output struct
├── config.go             ← hooks.yaml 구조체, 로딩 (정적, 핫리로드는 Phase G)
├── snapshot.go           ← getHooksConfigFromSnapshot 동등 (메모리 스냅샷)
├── matcher.go            ← "Bash(rm *)" 패턴 파서 + 매칭 평가
├── runner.go             ← 실행 오케스트레이터 (PreToolUse/PostToolUse 골격)
├── timeout.go            ← 글로벌 timeout 관리 (env CLAUDE_CODE_HOOK_TIMEOUT 동등)
└── backend/
    ├── interface.go      ← Backend interface { Run(ctx, input) (Output, error) }
    ├── command.go        ← shell exec backend
    ├── prompt.go         ← Fast tier LLM backend (★ tier 활용)
    ├── http.go           ← HTTP POST backend
    └── agent.go          ← mini-agent fork backend (Phase F의 subagent 활용 준비)

internal/context/
├── builder.go            ← FetchSystemPromptParts (errgroup 병렬)
├── system_prompt.go      ← static parts 조립
├── user_context.go       ← infractl.md 로딩 (기존 promptInputs.GetInfractlMD 재사용)
├── env_info.go           ← git, cwd, platform, date
├── boundary.go           ← DYNAMIC_BOUNDARY 마커 상수
└── assembler.go          ← static + boundary + dynamic 결합

internal/cache/prefix_marker.go
                          ← API cache_control 위치 결정 헬퍼 (실제 사용은 Phase B)

internal/executor/shell/                (★ 골격만, 실제 로직은 Phase E)
├── provider.go           ← ShellProvider interface
├── registry.go           ← shellProviders 맵
├── resolve.go            ← OS/SHELL → provider 선택
├── bash/
│   └── provider.go       ← 골격: 현재는 기존 executor/local.go 호출 위임
└── powershell/
    └── provider.go       ← 골격: 현재는 기존 executor/local.go 호출 위임

internal/agent/todo/
├── store.go              ← TodoItem (id, content, status, deps)
├── tool.go               ← TodoWrite/TodoRead 도구 (registry 등록)
├── tracker.go            ← 의존성 + 순서 강제 (in_progress 1개 제한)
└── render.go             ← TUI 표시용 마크다운 렌더러

internal/tools/todo_write.go
                          ← 기존 tools 레지스트리에 todo 도구 등록 wrapper
```

### 수정

```
internal/agent/loop_llm.go
  ← 도구 실행 직후 PostToolUse hook 호출 추가
    (PreToolUse 는 Phase D 까지 미사용; runner.go 가 빈 결과 반환하면 통과)

internal/tools/registry.go (혹은 동등 위치)
  ← TodoWrite/TodoRead 도구 등록

cmd/infractl/* (root.go 또는 main)
  ← hooks.yaml 로딩 부트스트랩 추가 (config 파일 없으면 빈 설정으로 진행)
```

### 제거 — 없음 (Phase A 무손상 원칙)

---

## 5. 소단계 작업

### A.1  hooks 패키지 골격 (event/config/matcher 만)
- claude_cli 참조: `schemas/hooks.ts:211-222`, `utils/hooks/hooksConfigSnapshot.ts:119-124`
- 작업:
  - `internal/hooks/event.go` — `HookEvent` 열거 (PreToolUse/PostToolUse/UserPromptSubmit/SessionStart/Stop/Notification/SubagentStop/Setup), `HookInput`/`HookOutput` struct
  - `internal/hooks/config.go` — yaml 로딩 (`HooksConfig` 구조체)
  - `internal/hooks/matcher.go` — `"Bash(rm *)"` 패턴 파싱 (Permission rule syntax 모방, glob + tool name)
- 산출물: 위 3개 파일
- 단위 테스트: matcher 표준 케이스 10+ (`Bash`, `Bash(git *)`, `Write`, `Write(*.yaml)` 등)

### A.2  hooks runner + 4 backend 인터페이스
- claude_cli 참조: `utils/hooks/execShellCommand`, `execPromptHook`, `execHttpHook`, `execAgentHook`
- 작업:
  - `internal/hooks/runner.go` — `RunPreToolUse / RunPostToolUse` (Phase A 시점에는 PostToolUse만 실제 호출 경로)
  - `internal/hooks/timeout.go` — env `INFRACTL_HOOK_TIMEOUT` (기본 30s)
  - `internal/hooks/backend/interface.go`
  - `command.go` — `os/exec` 기반 (단, 사용자 입력 직접 삽입 금지: 인자 분리)
  - `prompt.go` — Fast tier LLM (기존 llm 클라이언트 재사용)
  - `http.go` — `net/http` POST + 헤더 환경변수 보간 (allowedEnvVars 화이트리스트 명시)
  - `agent.go` — **골격만** (mini-agent fork는 Phase F에서 본격 구현; 현재는 stub + ErrNotImplemented)
- 산출물: `internal/hooks/{runner,timeout}.go` + `backend/*`
- 단위 테스트: 각 backend 의 happy path + timeout + 에러 wrap

### A.3  PostToolUse hook 활성화 (감사 로그)
- claude_cli 참조: `utils/hooks/hookEvents.ts:56-150`
- 작업:
  - `internal/agent/loop_llm.go` 에서 도구 실행 직후 `hooks.RunPostToolUse(ctx, input)` 호출
  - hooks.yaml 가 비어 있으면 no-op (회귀 0)
  - 기본 hooks.yaml 위치: `~/.infractl/hooks.yaml` (Phase A 에서 결정)
- 산출물: loop_llm.go diff
- 단위 테스트: yaml 없음 → no-op / yaml에 PostToolUse command → 호출 검증

### A.4  context 패키지 신설 + parallel fetch
- claude_cli 참조: `services/queryContext.ts:44-74`, `services/context.ts:36,170,155-189`, `utils/api.ts (splitSysPromptPrefix)`
- 작업:
  - `internal/context/builder.go` — `errgroup.Group` 으로 system_prompt + user_context + env_info 병렬 fetch
  - `internal/context/system_prompt.go` — 기존 `agent.BuildContextualLayout` 결과 wrap
  - `internal/context/user_context.go` — 기존 `promptInputs.GetInfractlMD` 재사용
  - `internal/context/env_info.go` — git status, cwd, platform, ISO date
  - `internal/context/boundary.go` — `DynamicBoundary = "__INFRACTL_DYNAMIC_BOUNDARY__"`
  - `internal/context/assembler.go` — `Assemble(static, dyn) string` (boundary 마커 삽입)
- ★ 이 단계에서는 기존 loop.go 의 시스템 프롬프트 조립과 **병렬 운영** (호출 안 함). 실제 사용은 Phase B 에서.
- 산출물: 위 6개 파일
- 단위 테스트: assembler 의 boundary 위치 검증, env_info 형식 회귀

### A.5  cache/prefix_marker
- claude_cli 참조: `utils/api.ts (splitSysPromptPrefix)`
- 작업:
  - `internal/cache/prefix_marker.go` — boundary 기준 prefix/suffix 분할 + cache_control 위치 결정 (Anthropic 백엔드 추가 시 사용 예정)
- 단위 테스트: boundary 없는 경우 / 여러 개인 경우 / 비어 있는 경우

### A.6  executor/shell 골격 (실제 로직은 Phase E)
- claude_cli 참조: `utils/shell/shellProvider.ts:1-34`, `bashProvider.ts:58-252`, `powershellProvider.ts:27-124`
- 작업:
  - `internal/executor/shell/provider.go` — `ShellProvider` interface (`BuildExecCommand`, `GetSpawnArgs`, `GetEnvironmentOverrides`)
  - `internal/executor/shell/registry.go` — 글로벌 등록 맵
  - `internal/executor/shell/resolve.go` — OS/SHELL 환경변수 → provider 선택
  - `internal/executor/shell/bash/provider.go` — **현재는 기존 `executor/local.go` 의 bash 분기를 호출하는 thin wrapper**
  - `internal/executor/shell/powershell/provider.go` — 동일하게 thin wrapper
- ★ 이 단계에서는 `executor/local.go` 가 직접 사용 → wrapper 호출로만 교체 (동작 동일)
- 산출물: 위 5개 파일 + `executor/local.go` 수정 (provider.BuildExecCommand 호출)
- 단위 테스트: provider 선택 로직 (Windows / Linux / `$SHELL=zsh` 등)

### A.7  agent/todo 패키지 + TodoWrite/TodoRead 도구
- claude_cli 참조: `tools/AgentTool/runAgent.ts:1-83` (LLM 작업 분해 패턴), 또한 Claude Code TodoWrite 도구 동작 (실제 외부 사양 준수)
- 작업:
  - `internal/agent/todo/store.go` — `TodoItem{ID, Content, Status: pending|in_progress|completed, Deps []int}`, in-memory 세션 store
  - `internal/agent/todo/tracker.go` — `Set`/`Update` 시 invariant 검증:
    - in_progress 1개 제한
    - deps 미충족 시 in_progress 거부
    - completed 후 status 변경 금지
  - `internal/agent/todo/tool.go` — `TodoWrite` (전체 목록 set), `TodoRead` (조회) 도구 정의
  - `internal/agent/todo/render.go` — 마크다운 체크리스트 (TUI 표시)
  - `internal/tools/todo_write.go` — 레지스트리 등록 wrapper
- 산출물: 위 5개 파일
- 단위 테스트: invariant 검증 (deps 위반, in_progress 중복, 잘못된 transition)

### A.8  tools 레지스트리 + system prompt 안내 (TodoWrite 사용 유도)
- 작업:
  - 기존 system prompt 어셈블리에 "복잡 다단계 작업은 TodoWrite 사용" 한 줄 주입 (실제 강제는 Phase G의 hook)
  - 도구 카탈로그에 todo_write/todo_read 노출
- 산출물: 시스템 프롬프트 섹션 추가 1곳

---

## 6. CLAUDE.md 규칙 준수 포인트

- [ ] `internal/hooks/runner.go` 가 300줄 초과하지 않도록 backend 분할 (이미 backend/ 하위로 분리됨)
- [ ] 모든 신규 .go 파일 File header DocBlock 의무
- [ ] `os/exec` 호출 시 사용자 입력 인자 분리 (`exec.Command(name, args...)`) — 쉘 문자열 결합 절대 금지
- [ ] hook 의 환경변수 보간은 **화이트리스트** 만 허용 (claude_cli 의 allowedEnvVars 패턴 동등)
- [ ] hooks.yaml 로딩 시 권한 체크 (group/world writable 거부 — 임의 hook 실행 위험)
- [ ] slog 사용. hook 실행 결과에 크리덴셜 누출 가능 → stdout/stderr 로그 시 마스킹 옵션 노출
- [ ] context.Context 첫 인자

---

## 7. 검증 방법

### 단위 테스트
- `internal/hooks/matcher_test.go` — 패턴 10+ 케이스
- `internal/hooks/backend/{command,prompt,http}_test.go` — happy path + timeout + 에러 wrap
- `internal/hooks/runner_test.go` — yaml 비어 있음 / PostToolUse 매칭 / 매칭 없음
- `internal/context/{assembler,env_info}_test.go` — boundary/형식 회귀
- `internal/cache/prefix_marker_test.go` — split 케이스
- `internal/executor/shell/resolve_test.go` — OS/SHELL 분기
- `internal/agent/todo/tracker_test.go` — invariant 위반 케이스 6+

### 통합 테스트
- `//go:build integration`
- 실제 hooks.yaml 파일 + 실제 외부 명령 실행 (echo 등)
- HTTP backend → 로컬 mock 서버 (httptest)

### E2E 시나리오
- 시나리오 1: hooks.yaml 없음 → infractl 정상 작동, PostToolUse no-op
- 시나리오 2: PostToolUse 에 echo command → 실제 실행 + stdout 로그 확인
- 시나리오 3: hooks.yaml 권한 0666 → 거부 + 에러 로그
- 시나리오 4: TodoWrite 호출 시나리오 (LLM 가짜 응답으로 todo 3개 생성 → tracker 검증)
- 시나리오 5: 골든 회귀 — 기존 시나리오 (서버 등록, SSH 접속, DB 쿼리 1건) 동작 동일

### 빌드
- `go build -o bin/infractl.exe ./cmd/infractl/`
- 회귀 0 (실행 결과 동일)

---

## 8. 종료 조건

- [ ] §7 단위/통합 테스트 100% 통과
- [ ] §7 E2E 시나리오 5개 모두 통과
- [ ] 기존 `go test ./...` 회귀 0
- [ ] PostToolUse hook이 실제 작동 (echo 시나리오)
- [ ] TodoWrite/TodoRead 도구가 LLM 도구 카탈로그에 노출
- [ ] `docs/infractl-architecture.md` "재설계 마이그레이션" 섹션 갱신
- [ ] `docs/design/hooks.md` 의 Phase A 부분 잠금
- [ ] `docs_mig/README.md` 진행 현황 표 update
- [ ] PreToolUse 는 호출 경로만 준비 (실제 활성화는 Phase D)

---

## 9. 다음 phase (B) 진입 전 사용자 질문 항목

```
[ ] Q1. query.Engine 골든 시나리오 회귀 테스트 데이터 — 어디 저장? (testdata/golden/?)
[ ] Q2. 기존 LLM 루프 제거 범위 — `loop_llm.go` 제거까지 Phase B에 포함?
[ ] Q3. 분류 단계가 query 진입 전 1회만 — 동의?
       (turn 마다 재분류 안 함, 사용자 메시지가 추가될 때만 재분류)
[ ] Q4. streaming → TUI handler 콜백 인터페이스 변경 허용 여부
       (현재 a.handler.OnToken/OnThinkingToken/OnUsageUpdate)
```

---

## 끝.
