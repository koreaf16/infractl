# docs_mig — InfraCtl ↔ claude_cli 흡수 마이그레이션 인덱스

## 1. 이 디렉토리는?

InfraCtl 을 **claude_cli 패턴 기반으로 재설계**하기 위한 페이즈별 실행 문서 모음.

- **실행 계획** 은 본 디렉토리 (`docs_mig/`) 에만 둔다.
- **아키텍처/디자인 문서** 는 `docs/` 와 `docs/design/` 에 둔다.
- 각 phase 는 **별도 세션** 에서 진행 — 진입 전 사용자 질문 → 승인 후 코드 작업.

### 보존 / 흡수 / 제거 3 대 원칙

```
✅ 보존 (차별점)
  ① LLM 분류 단계 (약한 LLM 에서도 토큰 절감)
  ② Shell 창 표시 (PTY 기반)
  ③ Korean UX
  ④ 기존 circuit breaker, useInlineToolCalls

🔄 흡수 (claude_cli 패턴 이식)
  Query Engine state machine + streaming + 4중 compaction + Hook 시스템
  ShellProvider + bash AST + 자동 백그라운딩 + Monitor + subagent 병렬
  TodoWrite + Plan Mode + 사용자 hook + DYNAMIC_BOUNDARY 캐시

❌ 제거 (동등성 검증 후)
  preflight/{validator, shell_precheck, structured_guard} — hook 흡수
```

---

## 2. 문서 일람

| 파일 | 역할 | 상태 |
|---|---|---|
| [`00_overview.md`](00_overview.md) | 전체 비전 + 보존/흡수/제거 매트릭스 + phase 의존성 | ✅ |
| [`conventions.md`](conventions.md) | claude_cli 소스 참조 규칙 + TS→Go 매핑 | ✅ |
| [`checklist_per_phase.md`](checklist_per_phase.md) | phase 공통 템플릿 9 섹션 | ✅ |
| [`01_phase_a_infrastructure.md`](01_phase_a_infrastructure.md) | Phase A: hooks/context/cache/shell 골격 | ✅ 계획 완료 |
| [`02_phase_b_query_engine.md`](02_phase_b_query_engine.md) | Phase B: query state machine + streaming | ✅ 완료 |
| [`03_phase_c_compaction.md`](03_phase_c_compaction.md) | Phase C: 4중 compaction stack + recovery | ✅ 계획 완료 |
| [`04_phase_d_hooks_migration.md`](04_phase_d_hooks_migration.md) | Phase D: precheck → hook 이관, preflight 제거 | ✅ 계획 완료 |
| [`05_phase_e_shell_provider.md`](05_phase_e_shell_provider.md) | Phase E: bash AST + PowerShell EncodedCommand | ✅ 계획 완료 |
| [`06_phase_f_multitask.md`](06_phase_f_multitask.md) | Phase F: 백그라운드 / Monitor / subagent 병렬 / 스케줄 | ✅ 계획 완료 |
| [`07_phase_g_planmode_todo.md`](07_phase_g_planmode_todo.md) | Phase G: Plan Mode + hook 핫리로드 + CLI | ✅ 계획 완료 |

---

## 3. Phase 진행 현황 (실행 추적)

각 phase 실제 착수 / 완료 시 본 표를 갱신한다.

| Phase | 이름 | 계획 | 진입 | 완료 | 주요 산출물 |
|---|---|---|---|---|---|
| A | Infrastructure (hooks/context/cache/shell/todo 골격) | ✅ | ✅ | ✅ | `internal/hooks/`, `internal/context/`, `internal/cache/prefix_marker`, `internal/executor/shell/*`, `internal/agent/todo/` |
| B | Query Engine (state machine + streaming) | ✅ | ✅ | ✅ | `internal/agent/query/*`, 기존 루프 제거 |
| C | Compaction 4중 stack | ✅ | ✅ | ✅ | `internal/agent/compact/*` |
| D | Precheck → Hook 완전 이관 | ✅ | ✅ | ✅ | HookOutput 스키마 교체, PreToolUse 활성화, system_risk.sh+shell_validator.md, hooks.yaml.default 부트스트랩, preflight 제거, TUI 간결화, 동등성 55 케이스 |
| E | Shell Provider (bash AST + PS EncodedCommand) | ✅ | ✅ | ✅ | `internal/executor/shell/bash/{parser,heredoc,semantics,danger,readonly,specs,snapshot,quoting,pipe}`, powershell EncodedCommand, `InjectNonInteractiveFlags` 제거, 55/55 동등 |
| F | 멀티태스크 + 다단계 보강 | ✅ | ✅ | ✅ | `internal/background/*`, `internal/tools/monitor.go`, `internal/subagent/parallel.go`, `internal/schedule/{oneshot,retention,log}.go` |
| G | Plan Mode + 사용자 hook + CLI | ✅ | ✅ | ✅ | `internal/agent/planmode/*`, `internal/hooks/{watcher,reloader}.go`, `internal/agent/todo/{signals,prompt_injector,enforcer}.go`, `cmd/infractl/{hooks,plan}.go`, `docs/design/todo-planmode.md` |

**진행 순서**: A → B → (C, D, F 병렬 가능) → E → G

**병렬 가능성 규칙**: B 완료 후 C/D/F 동시 진행 가능. 단 본인 이해도에 따라 직렬로 진행해도 무방.

---

## 4. 각 phase 진입 전 체크리스트

[`checklist_per_phase.md`](checklist_per_phase.md) 에 상세 템플릿 있음. 요지:

```
[ ] 1. 이전 phase §7 검증 시나리오 모두 통과?
[ ] 2. 이전 phase §8 종료 조건 충족?
[ ] 3. 다음 phase §9 질문 항목에 사용자 답변?
[ ] 4. claude_cli 참조 소스 (§2) 전부 Read 완료?
[ ] 5. `docs/design/<해당>.md` 작성 (없다면 phase 진입 직전 작성)?
[ ] 6. CLAUDE.md / conventions.md 재확인?
```

---

## 5. 디자인 문서 상태 (`docs/design/`)

| 파일 | 목적 | 작성 시점 | 상태 |
|---|---|---|---|
| `redesign-overview.md` | 전체 비전 1장 | 본 세션 (Phase A 진입 전) | ✅ |
| `hooks.md` | Hook 시스템 상세 (Phase A/D 사용) | 본 세션 | ✅ |
| `query-engine.md` | Query 상태기계 상세 | Phase B 완료 시 반영 | ✅ |
| `compaction.md` | 4중 compaction 상세 | Phase C 진입 직전 | ⏳ |
| `shell-provider.md` | Shell Provider + bash AST 상세 | Phase E 진입 직전 | ⏳ |
| `multitask.md` | 멀티태스크 상세 | Phase F 진입 직전 | ⏳ |
| `todo-planmode.md` | TodoWrite + Plan Mode 상세 | Phase G 완료 | ✅ |

---

## 6. 관련 문서

- [`../docs/infractl-architecture.md`](../docs/infractl-architecture.md) — 현재 아키텍처 (각 phase 완료 시 갱신)
- [`../docs/design/redesign-overview.md`](../docs/design/redesign-overview.md) — 재설계 전체 비전
- [`../docs/design/hooks.md`](../docs/design/hooks.md) — Hook 시스템 상세
- [`../CLAUDE.md`](../CLAUDE.md) — 프로젝트 규칙 (300 줄 제한, DocBlock, slog, 등)

---

## 7. 마이그레이션 완료

Phase A–G 전부 구현 완료 (2026-04-18).

claude_cli 흡수 마이그레이션은 이 단계에서 종료된다. 이후 작업은 별도 Phase 로 분리한다.

- **Phase 7**: Daemon + Web UI
- **Phase 8**: 모니터링 + 알림

---

## 끝.
