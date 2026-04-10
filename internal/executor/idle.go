// Package executor
// File: idle.go
// Description: 명령 출력 유휴 상태 감지 타이머
// Responsibility: 스트리밍 실행 중 일정 시간 출력이 없으면 Triggered 채널을 닫아 알림

package executor

import (
	"context"
	"time"
)

const defaultIdleThreshold = 10 * time.Second

// IdleWatcher는 마지막 Ping 이후 threshold 시간 동안 추가 Ping이 없으면
// Triggered() 채널을 닫아 호출자에게 유휴 상태를 알린다.
// 명령이 stdin을 기다리며 멈춘 상황을 감지하는 데 사용한다.
type IdleWatcher struct {
	threshold time.Duration
	resetCh   chan struct{}
	triggerCh chan struct{}
	stopCh    chan struct{}
}

// NewIdleWatcher는 지정된 threshold로 IdleWatcher를 생성한다.
// threshold가 0이면 defaultIdleThreshold(10초)를 사용한다.
func NewIdleWatcher(threshold time.Duration) *IdleWatcher {
	if threshold == 0 {
		threshold = defaultIdleThreshold
	}
	return &IdleWatcher{
		threshold: threshold,
		resetCh:   make(chan struct{}, 16), // 버퍼: 빠른 라인 스트림에서 블로킹 방지
		triggerCh: make(chan struct{}),
		stopCh:    make(chan struct{}),
	}
}

// Start는 백그라운드 감시 고루틴을 시작한다. 반드시 한 번만 호출해야 한다.
// ctx가 취소되거나 Stop이 호출되면 고루틴이 종료된다.
func (w *IdleWatcher) Start(ctx context.Context) {
	go w.run(ctx)
}

// Stop은 감시 고루틴을 종료한다.
func (w *IdleWatcher) Stop() {
	select {
	case <-w.stopCh:
		// 이미 종료됨
	default:
		close(w.stopCh)
	}
}

// Ping은 새 출력 라인이 수신될 때마다 호출한다. 유휴 타이머를 리셋한다.
func (w *IdleWatcher) Ping() {
	select {
	case w.resetCh <- struct{}{}:
	default:
		// 버퍼가 가득 찬 경우 드롭 — 고루틴이 곧 소비하므로 안전
	}
}

// Triggered는 유휴 threshold 초과 시 닫히는 채널을 반환한다.
// 한 번만 닫힌다. 이후 Ping은 무시된다.
func (w *IdleWatcher) Triggered() <-chan struct{} {
	return w.triggerCh
}

func (w *IdleWatcher) run(ctx context.Context) {
	timer := time.NewTimer(w.threshold)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-w.resetCh:
			// 보류 중인 reset을 모두 소비한 뒤 타이머 재시작
			drainResets(w.resetCh)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(w.threshold)
		case <-timer.C:
			// threshold 초과 → 한 번만 닫음
			select {
			case <-w.triggerCh:
				// 이미 닫혀 있음 (중복 방지)
			default:
				close(w.triggerCh)
			}
			return
		}
	}
}

// drainResets는 resetCh에 쌓인 신호를 모두 비운다.
func drainResets(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
