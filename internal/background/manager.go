// Package background
// File: manager.go
// Description: 백그라운드 작업 생명주기 관리자
// Responsibility: 작업 등록, goroutine 실행, 완료 알림, 목록/취소/스트리밍 제공

package background

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// NotifyFunc는 작업 완료 시 호출되는 콜백 타입이다.
type NotifyFunc func(jobID int, description string, success bool)

// Manager는 백그라운드 작업의 생명주기를 관리한다.
type Manager struct {
	mu          sync.Mutex
	jobs        map[int]*jobEntry
	nextID      int
	notifyFunc  NotifyFunc
	storageDir  string // Phase F: 파일 스트리밍 저장 경로
	maxFileSize int64  // Phase F: 파일 최대 크기 (회전 임계)
}

type jobEntry struct {
	Job
	cancel context.CancelFunc
}

// NewManager는 새 Manager를 생성한다. storageDir가 비어 있으면 파일 스트리밍을 비활성화한다.
func NewManager(storageDir string, maxFileSize int64) *Manager {
	return &Manager{
		jobs:        make(map[int]*jobEntry),
		nextID:      1,
		storageDir:  storageDir,
		maxFileSize: maxFileSize,
	}
}

// SetNotifyFunc는 작업 완료 시 호출할 콜백을 설정한다.
func (m *Manager) SetNotifyFunc(fn NotifyFunc) {
	m.mu.Lock()
	m.notifyFunc = fn
	m.mu.Unlock()
}

// Submit은 fn을 백그라운드 goroutine으로 실행하고 작업 ID를 반환한다.
// 기존 memory-only 호출자(RAG 등) 보호를 위해 유지한다.
func (m *Manager) Submit(ctx context.Context, description string, fn func(ctx context.Context) (string, error)) int {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	jobCtx, cancel := context.WithCancel(ctx)
	e := &jobEntry{
		Job: Job{
			ID:          id,
			Description: description,
			Status:      StatusRunning,
			StartedAt:   time.Now(),
		},
		cancel: cancel,
	}
	m.jobs[id] = e
	m.mu.Unlock()

	go func() {
		result, err := fn(jobCtx)
		now := time.Now()

		m.mu.Lock()
		e.CompletedAt = &now
		if err != nil {
			e.Status = StatusFailed
			e.Error = err.Error()
		} else {
			e.Status = StatusCompleted
			e.Result = result
		}
		notify := m.notifyFunc
		m.mu.Unlock()

		success := err == nil
		slog.Info("background job completed", "id", id, "success", success)
		if notify != nil {
			notify(id, description, success)
		}
	}()

	return id
}

// SubmitStreaming은 fn을 백그라운드로 실행하고 stdout/stderr를 파일에 기록한다.
// storageDir가 비어 있으면 파일 없이 실행하고 streams의 writer는 io.Discard로 대체한다.
// fn은 완료 시 streams에 쓰기를 중단해야 한다.
func (m *Manager) SubmitStreaming(ctx context.Context, description string, fn func(ctx context.Context, streams *JobStreams) error) int {
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	jobCtx, cancel := context.WithCancel(ctx)
	e := &jobEntry{
		Job: Job{
			ID:          id,
			Description: description,
			Status:      StatusRunning,
			StartedAt:   time.Now(),
			Streamed:    true,
		},
		cancel: cancel,
	}
	if m.storageDir != "" {
		e.StoragePath = jobStdoutPath(m.storageDir, id)
	}
	m.jobs[id] = e
	m.mu.Unlock()

	go func() {
		streams, stdoutCloser, stderrCloser, openErr := m.openStreams(id)
		if openErr != nil {
			slog.Error("failed to open job stream files", "id", id, "err", openErr)
			// 파일을 열지 못해도 fn은 실행 — streams는 io.Discard 대체
		}

		err := fn(jobCtx, streams)
		now := time.Now()

		// 파일 닫기 및 상태 기록
		if stdoutCloser != nil {
			stdoutCloser.Close()
		}
		if stderrCloser != nil {
			stderrCloser.Close()
		}
		if m.storageDir != "" {
			errMsg := ""
			status := StatusCompleted
			if err != nil {
				status = StatusFailed
				errMsg = err.Error()
			}
			if writeErr := writeStatusFile(m.storageDir, id, status, errMsg); writeErr != nil {
				slog.Warn("failed to write status file", "id", id, "err", writeErr)
			}
		}

		m.mu.Lock()
		e.CompletedAt = &now
		if err != nil {
			e.Status = StatusFailed
			e.Error = err.Error()
		} else {
			e.Status = StatusCompleted
		}
		notify := m.notifyFunc
		m.mu.Unlock()

		success := err == nil
		slog.Info("background streaming job completed", "id", id, "success", success)
		if notify != nil {
			notify(id, description, success)
		}
	}()

	return id
}

// RegisterPending은 이미 외부 goroutine에서 실행 중인 작업을 Manager에 등록한다.
// 반환된 complete 함수를 goroutine 종료 시 반드시 호출해야 한다.
// Auto-promotion에서 30s 초과 후 기존 goroutine 결과를 추적하는 데 사용한다.
func (m *Manager) RegisterPending(description string) (id int, complete func(result string, err error)) {
	m.mu.Lock()
	id = m.nextID
	m.nextID++
	e := &jobEntry{
		Job: Job{
			ID:          id,
			Description: description,
			Status:      StatusRunning,
			StartedAt:   time.Now(),
		},
		cancel: func() {}, // 외부 goroutine은 Manager가 취소할 수 없음
	}
	m.jobs[id] = e
	m.mu.Unlock()

	complete = func(result string, err error) {
		now := time.Now()
		m.mu.Lock()
		e.CompletedAt = &now
		if err != nil {
			e.Status = StatusFailed
			e.Error = err.Error()
		} else {
			e.Status = StatusCompleted
			e.Result = result
		}
		notify := m.notifyFunc
		m.mu.Unlock()

		success := err == nil
		slog.Info("promoted background job completed", "id", id, "success", success)
		if notify != nil {
			notify(id, description, success)
		}
	}
	return id, complete
}

// openStreams는 storageDir에 파일을 열고 JobStreams를 생성한다.
// storageDir가 비어 있으면 io.Discard 기반 streams를 반환한다.
func (m *Manager) openStreams(id int) (streams *JobStreams, stdoutCloser, stderrCloser io.Closer, err error) {
	if m.storageDir == "" {
		return &JobStreams{Stdout: io.Discard, Stderr: io.Discard}, nil, nil, nil
	}
	so, se, openErr := openJobFiles(m.storageDir, id, m.maxFileSize)
	if openErr != nil {
		return &JobStreams{Stdout: io.Discard, Stderr: io.Discard}, nil, nil, openErr
	}
	return &JobStreams{Stdout: so, Stderr: se}, so, se, nil
}

// AddMonitorBytes는 Monitor 도구가 jobID에 대해 방출한 바이트 수를 누적한다.
// 반환값은 갱신된 누적량이다.
func (m *Manager) AddMonitorBytes(id, n int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok {
		return 0
	}
	e.MonitorBytesEmitted += n
	return e.MonitorBytesEmitted
}

// StorageDir는 현재 설정된 스트리밍 저장 경로를 반환한다.
func (m *Manager) StorageDir() string {
	return m.storageDir
}

// List는 모든 작업의 스냅샷을 반환한다.
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.jobs))
	for _, e := range m.jobs {
		out = append(out, e.Job)
	}
	return out
}

// Get은 ID로 작업을 조회한다.
func (m *Manager) Get(id int) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok {
		return Job{}, fmt.Errorf("작업 #%d 를 찾을 수 없습니다", id)
	}
	return e.Job, nil
}

// Cancel은 실행 중인 작업을 취소한다.
func (m *Manager) Cancel(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("작업 #%d 를 찾을 수 없습니다", id)
	}
	if e.Status != StatusRunning {
		return fmt.Errorf("작업 #%d 는 이미 종료되었습니다 (%s)", id, e.Status)
	}
	e.cancel()
	e.Status = StatusCancelled
	now := time.Now()
	e.CompletedAt = &now

	if m.storageDir != "" {
		if writeErr := writeStatusFile(m.storageDir, id, StatusCancelled, "cancelled by user"); writeErr != nil {
			slog.Warn("failed to write cancel status file", "id", id, "err", writeErr)
		}
	}
	return nil
}

// CleanStorage는 보관 정책에 따라 오래된 파일을 삭제한다.
func (m *Manager) CleanStorage(keepDays, maxResults int) error {
	if m.storageDir == "" {
		return nil
	}
	return cleanOldFiles(m.storageDir, keepDays, maxResults)
}

