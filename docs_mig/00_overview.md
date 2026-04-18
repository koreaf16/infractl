# 00_overview.md — InfraCtl ↔ claude_cli 흡수 마이그레이션 전체 개요

## 1. 왜 하는가

InfraCtl 의 현 구조는 점진적으로 쌓여 다음 격차를 갖는다:

1. **Query Engine 격차** — 단순 `for i<50` 루프. claude_cli 는 state machine + streaming + 4중 compaction.
2. **정책 외부화 부재** — 모든 검증 로직이 Go 코드 안에 박혀 조직별 커스터마이징·외부 시스템 연동 불가.
3. **Shell 추상화 부재** — `executor/local.go` 안 OS 분기. AST 기반 위험 분석 없음.
4. **다단계 작업 추적 부재** — 설치 같은 multi-step 에서 순서/실패 전파 보장 메커니즘 없음.
5. **백그라운드/멀티태스크 미성숙** — `is_background` 미구현, 자동 백그라운딩·결과 파일화·진행 폴링 없음.

claude_cli 의 검증된 패턴을 흡수해 위 격차를 해소한다.

---

## 2. 보존 / 흡수 / 제거 매트릭스

```
┌─────────────────────────────────────────────────────────────────────┐
│ ✅ 보존 (우리 차별점, 절대 손대지 않음)                            │
│   ① LLM 분류 단계 (classify → tier → reasoning/fast/general)       │
│      — 약한 LLM(Qwen)도 분류로 토큰/비용 절감                       │
│   ② Shell 실행 시 shell 창 표시 (PTY interactive window)           │
│      — ConPTY/Unix PTY 그대로, 사용자 직접 관찰 UX                  │
│   ③ Korean UX, infractl 도메인 (서버/SSH/DB)                       │
│   ④ 기존 circuit breaker (compaction 안전성)                        │
│   ⑤ useInlineToolCalls (vLLM 우회)                                  │
│                                                                     │
│ 🔄 흡수 (claude_cli 패턴 그대로 이식)                              │
│   ① Query Engine: state machine + streaming                        │
│   ② Context Build: parallel fetch + DYNAMIC_BOUNDARY               │
│   ③ Compaction 4중 stack (auto + reactive + micro + collapse)      │
│   ④ Hook 시스템 (precheck/validator 흡수, 4 backend)               │
│   ⑤ ShellProvider 추상화 (bash/powershell)                         │
│   ⑥ Streaming Tool Executor (병렬 + immediate yield + sibling abort)│
│   ⑦ Mini-agent (hook agent backend, 위임 검증)                     │
│   ⑧ TodoWrite (다단계 작업 순서 보장)                              │
│   ⑨ Plan Mode (실행 전 승인)                                       │
│   ⑩ 자동 백그라운딩 + 결과 파일화 + 진행 폴링                      │
│   ⑪ Monitor 도구                                                    │
│   ⑫ 스케줄 고도화 (oneshot, retention, log)                        │
│                                                                     │
│ ❌ 제거 (Phase D 동등성 검증 후)                                    │
│   ① preflight/validator.go (LLM 검증 루프) → Hook agent 흡수       │
│   ② preflight/shell_precheck.go 결정론 → Hook command 흡수         │
│   ③ preflight/structured_guard.go → Hook prompt 흡수               │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 3. 페이즈 구성과 의존성

```
Phase A: 인프라 신설 (무손상)
    │   hooks/, context/, cache/prefix_marker, executor/shell/{골격}, agent/todo
    │
    ▼
Phase B: Query Engine 교체 (★ 가장 큰 리스크)
    │   agent/query/{engine,state,transition,recovery,streaming_executor}
    │   기존 루프 제거, query.Engine 단일 경로
    │
    ├──────────┬──────────────┐
    ▼          ▼              ▼
Phase C    Phase D        Phase F
compaction precheck→hook  multitask
4중 stack  완전 이관      백그라운드/Monitor/subagent병렬/스케줄
    │          │              │
    └──────────┴──────────────┘
               │
               ▼
Phase E: Shell Provider 완전 적용 (mvdan.cc/sh/v3)
               │
               ▼
Phase G: Plan Mode + 사용자 hook + CLI 서브커맨드
```

- C / D / F는 B 완료 후 **병렬 가능**.
- E는 D의 결과(hook 흡수)에 의존하지 않지만, A의 ShellProvider 골격 위에서 실제 구현이므로 A 후순위.
- G는 모든 기능이 자리 잡은 후 사용자 친화 layer.

---

## 4. 페이즈 요약 (각 phase 문서 ↔ 이 표)

| Phase | 문서 | 목표 한 줄 | 주요 신설 |
|---|---|---|---|
| A | `01_phase_a_infrastructure.md` | Hook/Context/Cache/Shell/Todo 골격 신설 (무손상) | `internal/hooks`, `internal/context`, `internal/cache/prefix_marker`, `internal/executor/shell/*`(골격), `internal/agent/todo` |
| B | `02_phase_b_query_engine.md` | Query state machine + streaming 직접 교체 | `internal/agent/query/*` |
| C | `03_phase_c_compaction.md` | 4중 compaction stack + recovery | `internal/agent/compact/*` |
| D | `04_phase_d_hooks_migration.md` | precheck/validator → hook 완전 이관, preflight/ 제거 | hook backend 활성화, `internal/preflight/*` 제거 |
| E | `05_phase_e_shell_provider.md` | bash AST + PowerShell EncodedCommand + snapshot/quoting/pipe | `internal/executor/shell/{bash,powershell,analysis}/*` 완성 |
| F | `06_phase_f_multitask.md` | 자동 백그라운딩 + Monitor + subagent 병렬 + 스케줄 고도화 | `internal/background/*`, `internal/tools/monitor.go`, `internal/subagent/parallel.go`, `internal/schedule/{oneshot,retention,log}.go` |
| G | `07_phase_g_planmode_todo.md` | Plan Mode + 사용자 hook 핫리로드 + CLI 서브커맨드 | `internal/agent/planmode/*`, `~/.infractl/hooks.yaml` watcher, `cmd/infractl hooks/plan` |

---

## 5. 위험 / 완화 (전 phase 공통)

| 위험 | 완화책 |
|---|---|
| Query Engine 전면 교체 회귀 | Phase B 골든 시나리오 + E2E 회귀 테스트 통과 후 기존 루프 제거 |
| Hook 잘못 작성 시 전체 차단/통과 | hook validation CLI(`infractl hooks test`) + 기본 fail-closed 모드 (Phase G) |
| Hook 외부 호출 지연 | timeout 강제 + async 옵션 + statusMessage |
| 분류 단계가 query loop와 충돌 | classify 는 query 진입 **전** 1회만 (turn마다 재실행 X) |
| 창 표시 로직이 ShellProvider와 결합 | Provider 는 명령 문자열까지만, spawn 은 별도 layer (`executor/pty`) |
| LLM 약함 → micro/collapse 요약 품질 저하 | 요약은 Fast tier(Qwen) + 실패 시 trim 폴백 (기존 강점 유지) |
| precheck 제거 = 정책 누락 | Phase D 동등성 테스트 통과 후에만 제거. hooks default bundle 제공 |
| TodoWrite 미사용 시 다단계 보장 깨짐 | Plan Mode + system prompt 강제 + Hook으로 mutation 도구 호출 시 todo 존재 검증 |
| 백그라운드 자동 전환이 의도와 어긋남 | 임계 시간 설정 가능 + tool_result 명시 알림 |
| Phase 순서 위반 | conventions.md §B 진입 체크리스트 강제 |

---

## 6. 마이그레이션 외 자산 (그대로 유지)

```
internal/agent/classify/*           ← LLM 분류 ★ 보존
internal/agent/intel/*              ← reasoning tier ★ 보존
internal/executor/pty/*             ← 창 표시 ★ 보존
internal/executor/interactive/*     ← ★ 보존
internal/llm/openai_inline.go       ← vLLM 우회 ★ 보존
internal/connector/ssh/*            ← SSH executor
internal/store/*                    ← SQLite 영속성
internal/agent/compaction.go 의 circuit breaker → Phase C에서 compact/breaker.go로 이전 (코드 그대로)
대부분의 internal/tools/*           ← hook 통합 외 그대로
```

---

## 7. 의존성 추가 (전체)

```
mvdan.cc/sh/v3                  — bash AST (Phase E)
gopkg.in/yaml.v3                — hooks.yaml 파싱 (Phase A) — 기존에 있을 가능성 검증 후
github.com/fsnotify/fsnotify    — hooks.yaml 핫리로드 (Phase G)
golang.org/x/sync/errgroup      — 병렬 fetch / subagent 병렬 (Phase A, F)
```

---

## 8. 산출물 (전 phase 종료 시)

```
internal/
├── agent/
│   ├── loop.go                  (얇아짐)
│   ├── query/                   (Phase B)
│   ├── compact/                 (Phase C)
│   ├── todo/                    (Phase A)
│   ├── planmode/                (Phase G)
│   ├── classify/                (보존)
│   └── intel/                   (보존)
├── context/                     (Phase A)
├── cache/                       (Phase A 보강)
├── hooks/                       (Phase A 골격, Phase D 활성, Phase G CLI)
│   └── backend/
├── executor/
│   ├── shell/                   (Phase A 골격, Phase E 완성)
│   │   ├── bash/
│   │   ├── powershell/
│   │   └── analysis/
│   ├── pty/                     (보존)
│   └── interactive/             (보존)
├── background/                  (Phase F)
├── schedule/                    (Phase F 강화)
├── subagent/                    (Phase F 병렬화)
└── tools/
    ├── shell_exec.go            (hook 통합)
    ├── todo_write.go            (Phase A)
    └── monitor.go               (Phase F)

(Phase D 후 제거: internal/preflight/*)
```

---

## 9. 참고 문서

- `conventions.md` — claude_cli 참조 / TS→Go 매핑 / CLAUDE.md 규칙 점검
- `checklist_per_phase.md` — 페이즈 표준 9개 섹션 + 진입/진행/종료 체크리스트
- `README.md` — 진행 현황 인덱스
- `docs/design/redesign-overview.md` — 전체 비전 (한 장)
- `docs/design/hooks.md` — Hook 시스템 상세 설계 (Phase A 즉시 사용)
- 그 외 `docs/design/{query-engine, compaction, shell-provider, multitask, todo-planmode}.md` — 각 phase 시작 전 별도 세션에서 작성

---

## 끝.
