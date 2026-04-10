// Package background
// File: manager.go
// Description: 백그라운드 작업 생명주기 관리자
// Responsibility: 작업 등록, goroutine 실행, 완료 알림, 목록/취소 제공

package background

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// NotifyFunc는 작업 완료 시 호출되는 콜백 타입이다.
type NotifyFunc func(jobID int, description string, success bool)

// Manager는 백그라운드 작업의 생명주기를 관리한다.
type Manager struct {
	mu         sync.Mutex
	jobs       map[int]*jobEntry
	nextID     int
	notifyFunc NotifyFunc
}

type jobEntry struct {
	Job
	cancel context.CancelFunc
}

// NewManager는 새 Manager를 생성한다.
func NewManager() *Manager {
	return &Manager{
		jobs:   make(map[int]*jobEntry),
		nextID: 1,
	}
}

// SetNotifyFunc는 작업 완료 시 호출할 콜백을 설정한다.
// runREPL/runTUI에서 EventHandler.OnJobComplete를 연결하는 데 사용한다.
func (m *Manager) SetNotifyFunc(fn NotifyFunc) {
	m.mu.Lock()
	m.notifyFunc = fn
	m.mu.Unlock()
}

// Submit은 fn을 백그라운드 goroutine으로 실행하고 작업 ID를 반환한다.
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
	return nil
}
