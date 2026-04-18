# Phase D — Precheck → Hook 완전 이관

## 1. 목표

`internal/preflight/{validator,shell_precheck,structured_guard}.go` 의 모든 정책을 **Phase A 에서 만든 hook 시스템으로 이관**하고, **동등성 검증 통과 후 preflight/ 디렉토리 완전 제거**.

종료 시점에 사용자가 체감할 변화:
- `~/.infractl/hooks.yaml` 으로 정책을 **재컴파일 없이** 변경/추가 가능.
- 외부 시스템(ITSM/OPA/SIEM) 연동 가능.
- 위험 명령 차단 동작은 100% 동일 (회귀 0).

---

## 2. claude_cli 참조 소스

| 영역 | claude_cli 경로 | 핵심 심볼 |
|---|---|---|
| PreToolUse 통합 | `claude_cli/src/services/tools/toolExecution.ts` | hook result 처리 (`approved`, `newInput`) |
| Bash command hook | `claude_cli/src/utils/hooks/execShellCommand.ts` | timeout, statusMessage, async, asyncRewake |
| Prompt hook | `claude_cli/src/utils/hooks/execPromptHook.ts` | $ARGUMENTS 보간, 모델 선택 (Haiku 기본) |
| HTTP hook | `claude_cli/src/utils/hooks/execHttpHook.ts` | $VAR 보간 화이트리스트 |
| Agent hook | `claude_cli/src/utils/hooks/execAgentHook.ts` | mini-agent fork 검증 |

→ Phase A 에서 backend 골격을 만들었지만, **본 phase 시작 전 위 4 파일을 다시 Read** 해서 실제 동작 디테일 확인.

---

## 3. 선행 조건

- [ ] Phase A 의 hooks 패키지 + 4 backend 골격 완성
- [ ] Phase B 의 `query.Engine.tool_invoker.go` 가 PreToolUse hook 호출 자리 마련
- [ ] (선택) Phase C 완료 시 통합 안정성 더 좋음
- [ ] §9 Phase B 종료 시 사용자 질문 답변 완료

---

## 4. 신설 / 수정 / 제거 파일

### 신설

```
internal/hooks/builtins/
├── system_risk.sh              ← rm -rf / 등 차단 (claude_cli 의 fast-path 동등)
├── protected_paths.sh          ← /etc, /root, /var/log 보호
├── tar_extract_check.sh        ← tar 추출 사전 검증
├── mkdir_parent_check.sh       ← mkdir 부모 검증
└── readme.md

internal/hooks/builtins/agent/
├── shell_validator.md          ← LLM agent hook 프롬프트 (기존 validatorSystemPrompt 이전)
└── structured_config.md        ← yaml/json 검증 프롬프트

~/.infractl/hooks.yaml.default  ← 설치 시 배포되는 기본 hook 번들
```

### 수정

```
internal/agent/query/tool_invoker.go (Phase B 산출물)
  ← PreToolUse hook 결과 적용 활성화 (이전엔 빈 결과 통과)

internal/tools/shell_exec.go
  ← preflight.Validator.Validate() 호출 제거
  ← (Phase B 의 query.Engine 이 hook 을 호출하므로 도구 자체는 신경 X)

cmd/infractl 설치 스크립트
  ← hooks.yaml.default 설치 위치 (~/.infractl/hooks.yaml 미존재 시 복사)
```

### 제거 (★ 동등성 검증 통과 후, 사용자 명시 승인 필수)

```
internal/preflight/validator.go
internal/preflight/shell_precheck.go
internal/preflight/structured_guard.go
internal/preflight/* (디렉토리 통째)
관련 probe_* 도구 (만약 다른 곳에서 안 쓰면)
```

---

## 5. 소단계 작업

### D.1  hook backend 정식 구현 (A 의 골격을 production-ready 로)
- claude_cli 참조: `execShellCommand.ts`, `execPromptHook.ts`, `execHttpHook.ts`, `execAgentHook.ts`
- 작업:
  - `internal/hooks/backend/command.go` — timeout, statusMessage, async, asyncRewake 옵션 추가
  - `internal/hooks/backend/prompt.go` — `$ARGUMENTS` 보간, 모델 선택 (Fast tier 기본)
  - `internal/hooks/backend/http.go` — 헤더 `$VAR` 보간 (allowedEnvVars 화이트리스트), 응답 JSON 파싱
  - `internal/hooks/backend/agent.go` — mini-agent fork 활성화 (Phase F 의 subagent 패키지 의존)
- 단위 테스트: 각 backend 정식 케이스 + edge case

### D.2  built-in 스크립트 (system_risk, protected_paths, tar, mkdir)
- 기존 `preflight/shell_precheck.go` 의 결정론 로직을 **shell 스크립트로 이전**
- 작업:
  - `internal/hooks/builtins/*.sh` 작성 (cross-platform 고려: bash + powershell 두 버전 또는 wrapper)
  - 입력: hook input JSON (stdin)
  - 출력: hook output JSON (stdout) — `{approved, reason, newInput?}`
- 단위 테스트: 각 스크립트 단독 실행 + 다양한 입력

### D.3  agent hook 프롬프트 (validator 흡수)
- claude_cli 참조: 기존 `internal/preflight/validator_prompt.go`
- 작업:
  - `internal/hooks/builtins/agent/shell_validator.md` — 기존 `validatorSystemPrompt` 이전
  - `internal/hooks/builtins/agent/structured_config.md` — structured_guard 의 검증 프롬프트
  - `agent.go` backend 가 이 프롬프트 + `probe_*` 도구로 mini-agent 실행
- 단위 테스트: 다양한 명령 입력 → agent hook 결과 검증

### D.4  hooks.yaml.default 작성
- 작업:
  - PreToolUse:
    - matcher `Bash(rm -rf /*)` → command `system_risk.sh`
    - matcher `Bash(*)` (if disk_modifying) → command `protected_paths.sh`
    - matcher `Bash(tar -*)` → command `tar_extract_check.sh`
    - matcher `Bash(mkdir *)` → command `mkdir_parent_check.sh`
    - matcher `Bash(*)` (if disk_modifying) → agent `shell_validator.md` (Fast tier, timeout 90s)
    - matcher `Write(*.yaml)` `Write(*.json)` → prompt `structured_config.md` (Fast tier)
  - PostToolUse:
    - matcher `Bash(*)` → http (옵션, 사용자 환경에서 비활성 default)
- 산출물: `~/.infractl/hooks.yaml.default` + 설치 스크립트
- 단위 테스트: yaml 로드 + 모든 hook entry 매칭 동작

### D.5  query.Engine PreToolUse 활성화
- 작업:
  - `internal/agent/query/tool_invoker.go` 에서 hook deny / newInput / approved 처리 활성화
  - hook 실패 (timeout/exception) 시 default 동작: **fail-closed** (안전 우선) — 단 hook 자체 결함과 정책 deny 구분
- 단위 테스트: 다양한 hook 결과 → 도구 실행/차단 케이스

### D.6  동등성 테스트 (★ 가장 중요)
- 작업:
  - 기존 preflight 가 차단/허용한 케이스 50+ 수집 (`testdata/equivalence/`)
  - 각 케이스를 기존 preflight + 신 hook 양쪽으로 실행
  - 결과 동등 (둘 다 차단 / 둘 다 허용 / reason 유사) 검증
- 산출물: `internal/agent/equivalence_test.go`
- 통과 기준: 50/50 동등

### D.7  preflight/* 제거 (★ 사용자 명시 승인 후)
- 작업:
  - 사용자에게 동등성 결과 보고
  - 승인 받으면 `internal/preflight/*` 제거
- ★ memory 규칙 (`feedback_deletion_workflow.md`): **삭제 전 항목 목록 보여주고 사용자 OK 받은 뒤 진행**

---

## 6. CLAUDE.md 규칙 준수 포인트

- [ ] hooks.yaml 권한 체크 (group/world writable 거부)
- [ ] hook command backend 호출 시 사용자 입력을 인자로 분리 (쉘 결합 금지)
- [ ] HTTP hook 의 환경변수 보간 화이트리스트 (allowedEnvVars)
- [ ] hook fail-closed 정책 명문화 (default: hook 자체 실패 시 도구 실행 차단)
- [ ] 모든 hook 호출에 timeout 강제 (default 30s)
- [ ] slog: hook name, matcher, result 구조화 로그
- [ ] 크리덴셜 노출 금지 — hook input 에 비밀이 있을 수 있음, 마스킹

---

## 7. 검증 방법

### 단위 테스트
- 각 backend 정식 케이스
- built-in 스크립트 단독 실행
- yaml 로드 + matcher 동작
- query.Engine hook 통합

### 통합 테스트
- `//go:build integration`
- 실제 hooks.yaml + 실제 외부 명령
- HTTP hook 로컬 mock 서버
- agent hook 실제 mini-agent fork

### E2E 시나리오
- 시나리오 1: `rm -rf /etc/*` → command hook block + reason 출력
- 시나리오 2: `rm -rf /tmp/foo` → 정책 통과
- 시나리오 3: `systemctl restart prod-db` (가상) → http hook (mock ITSM) 승인 대기 → 승인 후 실행
- 시나리오 4: yaml 변경 (Phase G의 핫리로드 전 — 재시작 필요) → 새 정책 반영
- 시나리오 5: hook 의도적 timeout → 도구 실행 차단 + 사유 명확
- 시나리오 6: agent hook (shell_validator) → 50 케이스 자동 회귀
- 시나리오 7: 동등성 50/50

### 빌드
- `go build -o bin/infractl.exe ./cmd/infractl/`
- 회귀 0

---

## 8. 종료 조건

- [ ] §7 모든 검증 통과
- [ ] 동등성 테스트 50/50
- [ ] 사용자에게 preflight/* 제거 목록 보고 + 승인 받음
- [ ] preflight/* 디렉토리 제거 완료
- [ ] hooks.yaml.default 가 설치 시 배포되도록 cmd/infractl 부트스트랩 완료
- [ ] `docs/design/hooks.md` Phase D 부분 갱신
- [ ] `docs/infractl-architecture.md` 갱신 (preflight 섹션 제거 + hooks 섹션 본격 기술)
- [ ] `docs_mig/README.md` update

---

## 9. 다음 phase (E / F) 진입 전 사용자 질문 항목

```
[ ] Q1. Phase E (Shell Provider 완성) 와 Phase F (멀티태스크) 순서 — E 먼저? F 먼저?
[ ] Q2. mvdan.cc/sh/v3 도입 동의 (Phase E 의존성)
[ ] Q3. PowerShell -EncodedCommand 전환 시 기존 -Command 사용 케이스 회귀 우려 점 있는지
[ ] Q4. 자동 백그라운딩 임계 — claude_cli 동일 (15s)? 우리 환경 (서버 작업이라 더 길어야 할 수도)
[ ] Q5. Monitor 도구의 출력량 제한 — 한 세션당 몇 KB?
```

---

## 끝.
