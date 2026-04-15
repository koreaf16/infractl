package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	shellBoxMaxLines     = 10
	shellBoxDisplayLines = 10
)

type boxIcon int

const (
	boxIconRunning boxIcon = iota
	boxIconDone
	boxIconFailed
)

func isShellBoxTool(name string) bool {
	return name == "shell_exec" || name == "file_transfer"
}

func renderShellBoxCompleted(name string, args map[string]any, lines []string, totalLines int, duration time.Duration, success bool, width int) string {
	icon := boxIconDone
	if !success {
		icon = boxIconFailed
	}
	return renderShellBoxContent(name, args, lines, totalLines, icon, duration, width)
}

func renderShellBoxRunning(name string, args map[string]any, lines []string, totalLines int, elapsed time.Duration, width int) string {
	return renderShellBoxContent(name, args, lines, totalLines, boxIconRunning, elapsed, width)
}

func renderShellBoxContent(name string, args map[string]any, outputLines []string, totalLines int, icon boxIcon, dur time.Duration, width int) string {
	if name == "discover_services" || name == "system_info" || name == "network_info" {
		return renderParallelTaskBox(name, args, outputLines, icon, dur, width)
	}
	if name == "discover_web_servers" {
		return renderDiscoverWebBox(name, args, outputLines, icon, dur, width)
	}

	boxW := width - 4
	if boxW < 30 {
		boxW = 30
	}
	innerW := boxW - 4

	var content strings.Builder
	content.WriteString(shellBoxStatusLine(name, args, totalLines, icon, dur))

	if len(outputLines) > shellBoxDisplayLines {
		outputLines = outputLines[len(outputLines)-shellBoxDisplayLines:]
	}
	for _, line := range outputLines {
		content.WriteString("\n" + truncateShellLine(line, innerW))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSubtle).
		PaddingLeft(1).
		PaddingRight(1).
		Width(boxW).
		Render(content.String())
}

func renderDiscoverServicesBox(name string, args map[string]any, lines []string, icon boxIcon, dur time.Duration, width int) string {
	boxW := width - 4
	if boxW < 30 {
		boxW = 30
	}
	innerW := boxW - 4

	var content strings.Builder
	content.WriteString(shellBoxStatusLine(name, args, 0, icon, dur))
	content.WriteString("\n")

	// Task status tracking
	type taskState struct {
		status string // "running", "done", "fail"
		info   string
	}
	tasks := make(map[string]*taskState)
	taskOrder := []string{"processes", "ports", "systemd", "docker"}
	for _, t := range taskOrder {
		tasks[t] = &taskState{status: "pending"}
	}

	// Parse lines for task updates
	for _, line := range lines {
		if strings.Contains(line, "task_start") {
			if t := extractField(line, "task"); t != "" {
				tasks[t].status = "running"
			}
		} else if strings.Contains(line, "task_done") {
			if t := extractField(line, "task"); t != "" {
				tasks[t].status = "done"
				count := extractField(line, "count")
				dur := extractField(line, "dur")
				tasks[t].info = fmt.Sprintf("Found %s items (%s)", count, dur)
			}
		} else if strings.Contains(line, "task_fail") {
			if t := extractField(line, "task"); t != "" {
				tasks[t].status = "fail"
				tasks[t].info = "Failed to scan"
			}
		}
	}

	for _, t := range taskOrder {
		ts := tasks[t]
		if ts.status == "pending" && (t == "systemd" || t == "docker") {
			continue // Skip if not applicable
		}

		var iconStr string
		var labelStyle lipgloss.Style
		switch ts.status {
		case "running":
			iconStr = StyleClaude().Render("●")
			labelStyle = StyleThinking
		case "done":
			iconStr = StyleSuccess.Render("\u221a")
			labelStyle = StyleCmdBoxDim
		case "fail":
			iconStr = StyleError.Render("x")
			labelStyle = StyleError
		default:
			iconStr = StyleCmdBoxDim.Render("○")
			labelStyle = StyleCmdBoxDim
		}

		line := fmt.Sprintf("  %s %-10s", iconStr, t)
		if ts.info != "" {
			line += " " + labelStyle.Render(ts.info)
		}
		content.WriteString("\n" + truncateShellLine(line, innerW))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSubtle).
		PaddingLeft(1).
		PaddingRight(1).
		Width(boxW).
		Render(content.String())
}

func extractField(line, key string) string {
	parts := strings.Split(line, " ")
	for _, p := range parts {
		if strings.HasPrefix(p, key+"=") {
			return strings.Trim(strings.TrimPrefix(p, key+"="), "\"")
		}
	}
	return ""
}

func shellBoxStatusLine(name string, args map[string]any, totalLines int, icon boxIcon, dur time.Duration) string {
	var iconStr string
	switch icon {
	case boxIconRunning:
		iconStr = StyleClaude().Render("●")
	case boxIconDone:
		iconStr = StyleSuccess.Render("\u221a")
	case boxIconFailed:
		iconStr = StyleError.Render("x")
	}

	line := iconStr + " " + StyleCmdBoxToolName.Render(toolDisplayName(name))
	if desc := shellBoxDescLine(name, args); desc != "" {
		line += StyleCmdBoxDim.Render(" -> ") + StyleClaude().Render(desc)
	}
	if isShellBoxTool(name) {
		line += StyleCmdBoxDim.Render(" -> ") + StyleCmdBoxDim.Render(fmt.Sprintf("%d lines", totalLines))
	}
	if dur > 0 && icon != boxIconRunning {
		line += " " + StyleCmdBoxDim.Render("("+formatElapsedShort(dur)+")")
	}
	return line
}

func shellBoxDescLine(name string, args map[string]any) string {
	return shellTaskLabel(name, args)
}

func buildRoundedBorderTitleLine(totalWidth int, title string) string {
	if totalWidth < 4 || title == "" {
		return ""
	}

	titleStr := StyleCmdBoxToolName.Render(title)
	innerWidth := totalWidth - 2
	titleWidth := lipgloss.Width(titleStr)
	if titleWidth >= innerWidth {
		plain := truncateStr(title, max(1, innerWidth-2))
		titleStr = StyleCmdBoxToolName.Render(plain)
		titleWidth = lipgloss.Width(titleStr)
	}

	leftFill := 1
	rightFill := innerWidth - leftFill - titleWidth
	if rightFill < 1 {
		rightFill = 1
		leftFill = max(0, innerWidth-rightFill-titleWidth)
	}
	if leftFill < 1 {
		leftFill = 1
	}

	borderStyle := lipgloss.NewStyle().Foreground(ColorSubtle)
	left := borderStyle.Render("?" + strings.Repeat("-", leftFill))
	right := borderStyle.Render(strings.Repeat("-", max(0, rightFill)) + "?")
	return left + titleStr + right
}

func appendShellLine(lines []string, line string, max int) []string {
	lines = append(lines, line)
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	return lines
}

func renderDiscoverWebBox(name string, args map[string]any, lines []string, icon boxIcon, dur time.Duration, width int) string {
	boxW := width - 4
	if boxW < 30 {
		boxW = 30
	}
	innerW := boxW - 4

	var content strings.Builder
	content.WriteString(shellBoxStatusLine(name, args, 0, icon, dur))
	content.WriteString("\n")

	type taskState struct {
		status string
		info   string
	}
	tasks := make(map[string]*taskState)
	taskOrder := []string{"web_servers", "web_ports", "ssl_certs", "vhosts"}
	for _, t := range taskOrder {
		tasks[t] = &taskState{status: "pending"}
	}

	for _, line := range lines {
		if strings.Contains(line, "task_start") {
			if t := extractField(line, "task"); t != "" {
				tasks[t].status = "running"
			}
		} else if strings.Contains(line, "task_done") {
			if t := extractField(line, "task"); t != "" {
				tasks[t].status = "done"
				count := extractField(line, "count")
				dur := extractField(line, "dur")
				tasks[t].info = fmt.Sprintf("Found %s items (%s)", count, dur)
			}
		} else if strings.Contains(line, "task_fail") {
			if t := extractField(line, "task"); t != "" {
				tasks[t].status = "fail"
				tasks[t].info = "Scan failed"
			}
		}
	}

	for _, t := range taskOrder {
		ts := tasks[t]
		var iconStr string
		var labelStyle lipgloss.Style
		switch ts.status {
		case "running":
			iconStr = StyleClaude().Render("●")
			labelStyle = StyleThinking
		case "done":
			iconStr = StyleSuccess.Render("\u221a")
			labelStyle = StyleCmdBoxDim
		case "fail":
			iconStr = StyleError.Render("x")
			labelStyle = StyleError
		default:
			iconStr = StyleCmdBoxDim.Render("○")
			labelStyle = StyleCmdBoxDim
		}

		displayName := strings.Title(strings.Replace(t, "_", " ", -1))
		line := fmt.Sprintf("  %s %-14s", iconStr, displayName)
		if ts.info != "" {
			line += " " + labelStyle.Render(ts.info)
		}
		content.WriteString("\n" + truncateShellLine(line, innerW))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSubtle).
		PaddingLeft(1).
		PaddingRight(1).
		Width(boxW).
		Render(content.String())
}

func renderParallelTaskBox(name string, args map[string]any, lines []string, icon boxIcon, dur time.Duration, width int) string {
	boxW := width - 4
	if boxW < 30 {
		boxW = 30
	}
	innerW := boxW - 4

	var content strings.Builder
	content.WriteString(shellBoxStatusLine(name, args, 0, icon, dur))
	content.WriteString("\n")

	// Determine tasks for this tool
	var taskOrder []string
	switch name {
	case "discover_services":
		taskOrder = []string{"processes", "ports", "systemd", "docker"}
	case "system_info":
		taskOrder = []string{"os", "cpu", "memory", "disk", "uptime"}
	case "network_info":
		taskOrder = []string{"interfaces", "routing", "arp"}
	}

	type taskState struct {
		status string // "running", "done", "fail"
		info   string
	}
	tasks := make(map[string]*taskState)
	for _, t := range taskOrder {
		tasks[t] = &taskState{status: "pending"}
	}

	// Parse lines for task updates
	for _, line := range lines {
		if strings.Contains(line, "task_start") {
			if t := extractField(line, "task"); t != "" {
				if _, ok := tasks[t]; ok {
					tasks[t].status = "running"
				}
			}
		} else if strings.Contains(line, "task_done") {
			if t := extractField(line, "task"); t != "" {
				if _, ok := tasks[t]; ok {
					tasks[t].status = "done"
					count := extractField(line, "count")
					dur := extractField(line, "dur")
					if count != "" {
						tasks[t].info = fmt.Sprintf("%s items found (%s)", count, dur)
					} else {
						tasks[t].info = fmt.Sprintf("Completed (%s)", dur)
					}
				}
			}
		} else if strings.Contains(line, "task_fail") {
			if t := extractField(line, "task"); t != "" {
				if _, ok := tasks[t]; ok {
					tasks[t].status = "fail"
					tasks[t].info = "Failed"
				}
			}
		}
	}

	for _, t := range taskOrder {
		ts := tasks[t]
		// Skip optional tasks that didn't run
		if ts.status == "pending" && (t == "systemd" || t == "docker") {
			continue
		}

		var iconStr string
		var labelStyle lipgloss.Style
		switch ts.status {
		case "running":
			iconStr = StyleClaude().Render("●")
			labelStyle = StyleThinking
		case "done":
			iconStr = StyleSuccess.Render("\u221a")
			labelStyle = StyleCmdBoxDim
		case "fail":
			iconStr = StyleError.Render("x")
			labelStyle = StyleError
		default:
			iconStr = StyleCmdBoxDim.Render("○")
			labelStyle = StyleCmdBoxDim
		}

		displayName := strings.Title(t)
		line := fmt.Sprintf("  %s %-12s", iconStr, displayName)
		if ts.info != "" {
			line += " " + labelStyle.Render(ts.info)
		}
		content.WriteString("\n" + truncateShellLine(line, innerW))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSubtle).
		PaddingLeft(1).
		PaddingRight(1).
		Width(boxW).
		Render(content.String())
}

func truncateShellLine(line string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	runes := []rune(line)
	if len(runes) <= maxWidth {
		return line
	}
	if maxWidth <= 3 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-3]) + "..."
}
