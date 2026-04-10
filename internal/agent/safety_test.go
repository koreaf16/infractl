package agent

import (
	"testing"

	"github.com/yourorg/infractl/internal/tools"
)

func TestEvaluateToolSafetyRequiresBackupForDestructiveShell(t *testing.T) {
	decision := evaluateToolSafety(&tools.ShellExecTool{}, map[string]interface{}{
		"command": "rm -rf /srv/app/current",
	})

	if decision.RiskLevel != tools.RiskHigh {
		t.Fatalf("expected high risk, got %s", decision.RiskLevel)
	}
	if !decision.BackupRequired {
		t.Fatalf("expected destructive shell command to require backup")
	}
}

func TestEvaluateToolSafetyRaisesMediumRiskForSingleFileDelete(t *testing.T) {
	decision := evaluateToolSafety(&tools.ShellExecTool{}, map[string]interface{}{
		"command": "rm /etc/app.conf",
	})

	if decision.RiskLevel != tools.RiskMedium {
		t.Fatalf("expected medium risk, got %s", decision.RiskLevel)
	}
	if !decision.BackupRequired {
		t.Fatalf("expected delete command to require backup")
	}
}
