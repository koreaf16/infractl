// Package executor
// File: executor.go
// Description: 명령 실행 인터페이스 및 실행 결과 타입 정의
// Responsibility: 명령 실행 추상화 인터페이스와 결과 타입 제공

package executor

import (
	"context"
	"time"
)

// ExecResult는 명령 실행의 결과를 나타낸다.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Executor는 쉘 명령을 실행하는 인터페이스이다.
// 구현체: LocalExecutor (로컬), SSHExecutor (Phase 2에서 구현)
type Executor interface {
	// Execute는 주어진 명령을 실행하고 결과를 반환한다.
	// 비정상 종료(exit code != 0)는 Go error가 아니라 ExecResult로 반환한다.
	// Go error는 실행 자체가 불가능한 경우(바이너리 없음, 컨텍스트 취소 등)에만 반환한다.
	Execute(ctx context.Context, command string) (ExecResult, error)

	// Target은 이 executor가 실행하는 대상 식별자를 반환한다.
	// LocalExecutor: "localhost", SSHExecutor: 서버명
	Target() string
}

// StreamExecutor는 실행 중 stdout을 라인 단위로 스트리밍하는 Executor이다.
// 구현체가 이 인터페이스를 만족하면 shell_exec 도구가 라이브 출력을 TUI에 전달한다.
type StreamExecutor interface {
	Executor

	// ExecuteStream은 명령을 실행하면서 stdout 라인마다 onLine 콜백을 호출한다.
	// onLine은 동기적으로 호출되며, 콜백이 느리면 실행이 지연될 수 있다.
	ExecuteStream(ctx context.Context, command string, onLine func(string)) (ExecResult, error)
}

// StdinInjector는 실행 중인 프로세스의 stdin에 데이터를 주입할 수 있는 executor이다.
// 명령이 인터랙티브 프롬프트(yes/no, 비밀번호 등)를 표시하며 멈췄을 때 응답을 보낸다.
// StreamExecutor를 구현하는 executor만 이 인터페이스를 구현한다.
type StdinInjector interface {
	// InjectStdin은 실행 중인 프로세스의 stdin에 line+"\\n"을 쓴다.
	// line이 빈 문자열이면 빈 줄(Enter)만 전송한다.
	// 프로세스가 종료되었거나 stdin 파이프가 없으면 error를 반환한다.
	InjectStdin(line string) error
}
