# InfraCtl 아키텍처 개요

## 목적
`infractl`은 로컬 또는 원격 서버를 대상으로 대화형 운영 작업을 수행하는 Go 기반 AI CLI다.  
기본 실행은 로컬이고, 원격 대상은 SSH를 통해 다룬다. Daemon 모드는 이후 단계에서 Web UI와 백그라운드 작업을 담당한다.

## 핵심 원칙
- Local-first: 별도 서버 없이 로컬에서 바로 실행한다.
- Remote via SSH: 원격 서버는 SSH로만 다룬다.
- LLM-driven tools: 사용자의 요청을 해석해 도구를 고르고 실행한다.
- Safety first: 위험 작업은 단계적으로 확인한다.
- Phase-based growth: 세부 구현은 각 페이즈 문서로 분리한다.

## 실행 모델
- `infractl`: 대화형 CLI
- `infractl daemon`: 백그라운드 서버, Web UI, API, 모니터링 담당

## 기본 흐름
1. 사용자가 명령을 입력한다.
2. 에이전트가 현재 환경과 대상 서버를 정리한다.
3. LLM이 필요한 도구와 실행 순서를 선택한다.
4. Tool이 Executor를 통해 로컬 또는 SSH 대상에서 실행된다.
5. 결과가 다시 LLM과 UI에 반영된다.

## 저장소 개요
- 설정: `~/.infractl/config.yaml`
- 로컬 상태/세션/이력: SQLite
- 서버 정보: SQLite
- 지식 검색/학습/후크: 각 페이즈에서 확장

## 세부 책임 분리
| 영역 | 주 책임 |
|---|---|
| Phase 1 | LLM 연동, 로컬 실행, 기본 REPL |
| Phase 2 | SSH, 서버 등록, SQLite 저장 |
| Phase 3 | TUI |
| Phase 4 | 서비스 디스커버리, 커넥터 |
| Phase 5 | 안전장치, 세션/이력 |
| Phase 6 | 웹 검색, 학습 도구, 커스텀 도구 |
| Phase 7 | RAG |
| Phase 8 | 서브에이전트, 체크포인트, 후크, 스케줄, 비용 |
| Phase 9 | Daemon, Web UI, API |
| Phase 10 | 모니터링, 로그 감시, 알림, 자동 모드 |

## 문서 기준
상세 설계는 이 문서에 두지 않고 각 페이즈 문서에 둔다.

## 관련 문서
- [Phase 1](./phase1-design.md)
- [Phase 2](./phase2-design.md)
- [Phase 3](./phase3-design.md)
- [Phase 4](./phase4-design.md)
- [Phase 5](./phase5-design.md)
- [Phase 6](./phase6-design.md)
- [Phase 7](./phase7-design.md)
- [Phase 8](./phase8-design.md)
- [Phase 9](./phase9-design.md)
- [Phase 10](./phase10-design.md)
