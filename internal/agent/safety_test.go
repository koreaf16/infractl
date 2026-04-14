// Package agent
// File: safety_test.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package agent

import (
	"testing"

	"github.com/yourorg/infractl/internal/tools"
)


func TestEvaluateToolSafetyHighRiskForDestructiveShell(t *testing.T) {
	decision := evaluateToolSafety(&tools.ShellExecTool{}, map[string]interface{}{
		"command": "rm -rf /srv/app/current",
	})

	if decision.RiskLevel != tools.RiskHigh {
		t.Fatalf("expected high risk, got %s", decision.RiskLevel)
	}
}

func TestEvaluateToolSafetyMediumRiskForSingleFileDelete(t *testing.T) {
	decision := evaluateToolSafety(&tools.ShellExecTool{}, map[string]interface{}{
		"command": "rm /etc/app.conf",
	})

	if decision.RiskLevel != tools.RiskMedium {
		t.Fatalf("expected medium risk, got %s", decision.RiskLevel)
	}
}

func TestEvaluateToolSafetyRaisesMediumRiskForInstallCommand(t *testing.T) {
	decision := evaluateToolSafety(&tools.ShellExecTool{}, map[string]interface{}{
		"command": "yum install -y oracle-database-preinstall-19c",
	})

	if decision.RiskLevel != tools.RiskMedium {
		t.Fatalf("expected medium risk, got %s", decision.RiskLevel)
	}
}

