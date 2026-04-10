// Package background
// File: types.go
// Description: 백그라운드 작업 도메인 타입 정의
// Responsibility: Job, JobStatus 타입 및 상태 상수 제공

package background

import "time"

// JobStatus는 백그라운드 작업의 현재 상태이다.
type JobStatus string

const (
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

// Job은 백그라운드에서 실행 중이거나 완료된 작업 하나를 나타낸다.
type Job struct {
	ID          int
	Description string
	Status      JobStatus
	Result      string
	Error       string
	StartedAt   time.Time
	CompletedAt *time.Time
}

// IsActive는 작업이 아직 실행 중인지 반환한다.
func (j *Job) IsActive() bool {
	return j.Status == StatusRunning
}
