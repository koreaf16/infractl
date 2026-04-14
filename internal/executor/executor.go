package executor

import (
	"context"
	"time"
)

// ExecResult captures the outcome of a command execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Executor runs a command on a target.
type Executor interface {
	Execute(ctx context.Context, command string) (ExecResult, error)
	Target() string
}

// StreamExecutor streams stdout line-by-line while the command is running.
type StreamExecutor interface {
	Executor
	ExecuteStream(ctx context.Context, command string, onLine func(string)) (ExecResult, error)
}

// StdinInjector injects input into the currently running command.
type StdinInjector interface {
	InjectStdin(line string) error
}

// InteractiveSpec describes an interactive command execution request.
type InteractiveSpec struct {
	Command    string
	RequirePTY bool
}

// InteractiveExecutor runs interactive commands while streaming raw terminal chunks.
type InteractiveExecutor interface {
	Executor
	StdinInjector
	ExecuteInteractive(ctx context.Context, spec InteractiveSpec, onChunk func(string)) (ExecResult, error)
}

// FileTransferExecutor uploads and downloads files using the underlying transport (e.g. SFTP over SSH).
// Avoids password prompts by reusing the already-authenticated connection.
type FileTransferExecutor interface {
	Executor
	Upload(ctx context.Context, localPath, remotePath string, onProgress func(transferred, total int64)) error
	Download(ctx context.Context, remotePath, localPath string, onProgress func(transferred, total int64)) error
}
