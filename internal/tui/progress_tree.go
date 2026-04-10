// Package tui
// File: progress_tree.go
// Description: Claude CLI 스타일 에이전트 진행 트리 렌더링
// Responsibility: 도구 실행 진행 상황을 트리 형태로 실시간 표시

package tui

import (
	"fmt"
	"strings"
	"time"
)

// 트리 문자 상수
const (
	treeBranch    = "├─"
	treeLastBranch = "└─"
	treePipe      = "│"
	treeResult    = "⎿"
	treeIndent    = "   "
	treeBranchPad = "   " // branch 후 들여쓰기
)

// progressStatus는 진행 항목의 상태를 나타낸다.
type progressStatus int

const (
	statusRunning progressStatus = iota
	statusDone
	statusError
)

// progressItem은 단일 도구 실행 항목이다.
type progressItem struct {
	toolID     string
	toolName   string
	target     string
	status     progressStatus
	duration   time.Duration
	startTime  time.Time
	lastOutput string
}

// progressTree는 현재 턴의 도구 실행 트리를 관리한다.
type progressTree struct {
	items []progressItem
	width int
}

// newProgressTree는 새 progressTree를 생성한다.
func newProgressTree() *progressTree {
	return &progressTree{}
}

// AddTool은 새 도구 실행을 트리에 추가한다.
func (pt *progressTree) AddTool(toolID, name, target string) {
	pt.items = append(pt.items, progressItem{
		toolID:    toolID,
		toolName:  name,
		target:    target,
		status:    statusRunning,
		startTime: time.Now(),
	})
}

// CompleteTool은 toolID에 해당하는 도구를 완료 처리한다.
func (pt *progressTree) CompleteTool(toolID string, duration time.Duration, success bool) {
	for i := range pt.items {
		if pt.items[i].toolID == toolID && pt.items[i].status == statusRunning {
			pt.items[i].duration = duration
			if success {
				pt.items[i].status = statusDone
			} else {
				pt.items[i].status = statusError
			}
			return
		}
	}
}

// SetOutput은 toolID 도구의 최근 출력을 설정한다.
func (pt *progressTree) SetOutput(toolID, line string) {
	for i := range pt.items {
		if pt.items[i].toolID == toolID && pt.items[i].status == statusRunning {
			pt.items[i].lastOutput = truncateStr(line, 60)
			return
		}
	}
}

// Reset은 트리를 초기화한다.
func (pt *progressTree) Reset() {
	pt.items = nil
}

// IsEmpty는 트리가 비어있는지 확인한다.
func (pt *progressTree) IsEmpty() bool {
	return len(pt.items) == 0
}

// RunningCount는 실행 중인 항목 수를 반환한다.
func (pt *progressTree) RunningCount() int {
	count := 0
	for _, item := range pt.items {
		if item.status == statusRunning {
			count++
		}
	}
	return count
}

// View는 진행 트리를 렌더링한다.
// 단일 도구: ● Running shell_exec... (3s)
// 복수 도구: ● 3 tool uses
//
//	├─ Shell · ls -la · ✓ (0.3s)
//	└─ Read · config.yaml
//	   ⎿  Reading file...
func (pt *progressTree) View() string {
	if len(pt.items) == 0 {
		return ""
	}

	var b strings.Builder

	// 헤더
	running := pt.RunningCount()
	if len(pt.items) == 1 && running == 1 {
		item := pt.items[0]
		elapsed := formatElapsedShort(time.Since(item.startTime))
		b.WriteString(treeIndent + StyleClaude().Render("●") + " ")
		b.WriteString(StyleThinking.Render("Running "+progressLabel(item)+"..."))
		b.WriteString(" " + StyleCmdBoxDim.Render("("+elapsed+")"))
		if item.lastOutput != "" {
			b.WriteString("\n" + treeIndent + treeBranchPad +
				StyleTreeLine.Render(treeResult) + "  " +
				StyleCmdBoxDim.Render(item.lastOutput))
		}
		return b.String()
	}

	// 복수 항목 헤더
	b.WriteString(treeIndent + StyleClaude().Render("●") + " ")
	if running > 0 {
		b.WriteString(StyleThinking.Render(fmt.Sprintf("Running %d tools...", running)))
	} else {
		b.WriteString(StyleInfoBarDim.Render(fmt.Sprintf("%d tool uses", len(pt.items))))
	}

	// 트리 항목
	for i, item := range pt.items {
		isLast := i == len(pt.items)-1
		b.WriteString("\n")
		b.WriteString(renderTreeItem(item, isLast))
	}

	return b.String()
}

// renderTreeItem은 단일 트리 항목을 렌더링한다.
func renderTreeItem(item progressItem, isLast bool) string {
	branch := treeBranch
	if isLast {
		branch = treeLastBranch
	}

	var b strings.Builder
	b.WriteString(treeIndent + treeBranchPad + StyleTreeLine.Render(branch) + " ")
	b.WriteString(StyleCmdBoxToolName.Render(item.toolName))

	if item.target != "" && item.target != "localhost" {
		b.WriteString(StyleCmdBoxDim.Render(" → ") + StyleCmdBoxTarget.Render(item.target))
	}

	// 상태 표시
	switch item.status {
	case statusDone:
		elapsed := formatElapsedShort(item.duration)
		b.WriteString(" " + StyleSuccess.Render("✓") + " " +
			StyleCmdBoxDim.Render("("+elapsed+")"))
	case statusError:
		elapsed := formatElapsedShort(item.duration)
		b.WriteString(" " + StyleError.Render("✗") + " " +
			StyleCmdBoxDim.Render("("+elapsed+")"))
	case statusRunning:
		elapsed := formatElapsedShort(time.Since(item.startTime))
		b.WriteString(" " + StyleCmdBoxDim.Render("("+elapsed+")"))
	}

	// 실행 중이고 출력이 있으면 하위 표시
	if item.status == statusRunning && item.lastOutput != "" {
		continueLine := treePipe
		if isLast {
			continueLine = " "
		}
		b.WriteString("\n" + treeIndent + treeBranchPad +
			StyleTreeLine.Render(continueLine+"  "+treeResult) + "  " +
			StyleCmdBoxDim.Render(item.lastOutput))
	}

	return b.String()
}

// progressLabel은 도구명과 대상을 결합한 표시 레이블을 반환한다.
func progressLabel(item progressItem) string {
	if item.target != "" && item.target != "localhost" {
		return item.toolName + " → " + item.target
	}
	return item.toolName
}
