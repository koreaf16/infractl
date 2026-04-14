// Package agent
// File: prompt_test.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package agent

import (
	"strings"
	"testing"
)

func TestAppendLocalControllerContextWindowsUsesPowerShellGuidance(t *testing.T) {
	var sb strings.Builder
	appendLocalControllerContext(&sb, "windows", "amd64", "WORKSTATION", `C:\Dev\Infractl`)
	got := sb.String()

	for _, want := range []string{
		"## Current Environment (Local Controller)",
		"- Local Shell: PowerShell",
		"- Command Guidance: Use PowerShell cmdlets and Windows paths by default.",
		`- Working Directory: C:\Dev\Infractl`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestAppendContextGuardrailsIncludesSessionContextRule(t *testing.T) {
	var sb strings.Builder
	appendContextGuardrails(&sb, true)
	got := sb.String()

	for _, want := range []string{
		"## Local vs Remote Guardrails",
		"`session_context`",
		"`this PC`, `local`, `the PC running infractl`",
		"`server_focus`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

func TestAppendSafetyRulesIncludeHereDocGuidance(t *testing.T) {
	var sb strings.Builder
	appendSafetyRules(&sb)
	got := sb.String()

	for _, want := range []string{
		"`<<EOF`",
		"`printf`",
		"escape `$`",
		"`sudo -n`",
		"`runuser -l`",
		"`bash -lc`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output:\n%s", want, got)
		}
	}
}

