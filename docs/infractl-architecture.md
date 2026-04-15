# InfraCtl 아키텍처 개요

## 목적
`infractl`은 로컬 또는 원격 서버를 대상으로 대화형 운영 작업을 수행하는 Go 기반 AI CLI다.  
기본 실행은 로컬이고, 원격 대상은 SSH를 통해 다룬다.

## 핵심 원칙
- Local-first: 별도 서버 없이 로컬에서 바로 실행한다.
- Remote via SSH: 원격 서버는 SSH로만 다룬다.
- LLM-driven tools: 사용자의 요청을 해석해 도구를 고르고 실행한다.
- Safety first: 위험 작업은 단계적으로 확인한다.

## 저장소 개요
- 설정: `~/.infractl/config.yaml`
- 상태 관리: SQLite (`internal/store`)
- 핵심 로직: `internal/` (agent, executor, connector, discovery 등)
- CLI 인터페이스: `cmd/`
- 빌드 결과물: `bin/`

## 주요 패키지 책임 (internal/)
| 패키지 | 책임 |
|---|---|
| `agent` | LLM 기반 메인 루프, 도구 실행 오케스트레이션 |
| `executor` | 로컬/원격 명령어 실행 및 입출력 제어 |
| `connector` | SSH 및 원격 환경 연결 관리 |
| `discovery` | 서비스 및 인프라 자원 식별 |
| `store` | 데이터 영속성 관리 (SQLite) |
| `tools` | 에이전트가 사용하는 기본 도구 세트 |
| `mcp` | Model Context Protocol 연동 |
| `subagent` | 복잡 작업 처리를 위한 위임 에이전트 |
| `tui` | 대화형 사용자 인터페이스 |
| `web` | 대시보드 및 API 서버 구현 |

