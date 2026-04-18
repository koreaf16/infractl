package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/yourorg/infractl/internal/executor"
)

type k8sQueryTestExecutor struct {
	mu       sync.Mutex
	commands []string
	results  map[string]executor.ExecResult
	errs     map[string]error
}

func (e *k8sQueryTestExecutor) Execute(_ context.Context, command string) (executor.ExecResult, error) {
	e.mu.Lock()
	e.commands = append(e.commands, command)
	e.mu.Unlock()

	if err, ok := e.errs[command]; ok {
		return executor.ExecResult{}, err
	}
	if result, ok := e.results[command]; ok {
		return result, nil
	}
	return executor.ExecResult{}, nil
}

func (e *k8sQueryTestExecutor) Target() string { return "localhost" }
func (e *k8sQueryTestExecutor) Host() string   { return "localhost" }

func TestParseK8sResourceList(t *testing.T) {
	args := map[string]interface{}{
		"resources": []interface{}{"deployments", " services "},
	}
	got := parseK8sResourceList(args, "pods,services")
	want := []string{"pods", "services", "deployments"}

	if len(got) != len(want) {
		t.Fatalf("parseK8sResourceList() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseK8sResourceList()[%d] = %q, want %q (%v)", i, got[i], want[i], got)
		}
	}
}

func TestK8sQueryToolExecuteMultiResourceGet(t *testing.T) {
	tool := &K8sQueryTool{}
	exec := &k8sQueryTestExecutor{
		results: map[string]executor.ExecResult{
			"kubectl get pods -n default -o wide":     {Stdout: "pod-a\npod-b"},
			"kubectl get services -n default -o wide": {Stdout: "svc-a"},
		},
	}

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":    "get",
		"resource":  "pods,services",
		"namespace": "default",
		"output":    "wide",
	}, exec)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(out.Content, "[pods]\npod-a") {
		t.Fatalf("expected pods section in output, got:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "[services]\nsvc-a") {
		t.Fatalf("expected services section in output, got:\n%s", out.Content)
	}

	if len(exec.commands) != 2 {
		t.Fatalf("expected 2 kubectl commands, got %d (%v)", len(exec.commands), exec.commands)
	}
}

func TestK8sQueryToolExecuteMultiResourceIncludesPerResourceErrors(t *testing.T) {
	tool := &K8sQueryTool{}
	exec := &k8sQueryTestExecutor{
		results: map[string]executor.ExecResult{
			"kubectl get pods --all-namespaces -o wide":     {Stdout: "pod-a"},
			"kubectl get services --all-namespaces -o wide": {ExitCode: 1, Stderr: "forbidden"},
		},
	}

	out, err := tool.Execute(context.Background(), map[string]interface{}{
		"action":   "get",
		"resource": "pods,services",
	}, exec)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(out.Content, "[pods]\npod-a") {
		t.Fatalf("expected pods section in output, got:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "[services]\nkubectl error (exit 1):\nforbidden") {
		t.Fatalf("expected services error section in output, got:\n%s", out.Content)
	}
}
