// Package tools
// File: k8s_query.go
// Description: Kubernetes 클러스터 리소스 조회 전용 도구
// Responsibility: kubectl get/describe/logs 등 k8s 조회 명령을 단일 도구로 대체

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
)

// K8sQueryTool은 Kubernetes 클러스터 리소스를 조회한다.
type K8sQueryTool struct{}

func (t *K8sQueryTool) Name() string { return "k8s_query" }

func (t *K8sQueryTool) Description() string {
	return "Query Kubernetes cluster resources (pods, services, deployments, nodes, events, logs).\n" +
		"Use this INSTEAD of shell_exec with 'kubectl get', 'kubectl describe', 'kubectl logs', " +
		"'kubectl top' commands.\n" +
		"Requires kubectl configured on the target. Supports namespace filtering and label selectors."
}

func (t *K8sQueryTool) IsReadOnly() bool     { return true }
func (t *K8sQueryTool) IsEnabled() bool      { return true }

func (t *K8sQueryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: get (list resources), describe (detail), logs (pod logs), top (resource usage). Default: get.",
				"enum":        []string{"get", "describe", "logs", "top"},
				"default":     "get",
			},
			"resource": map[string]interface{}{
				"type":        "string",
				"description": "Resource type: pods, services, deployments, nodes, events, namespaces, configmaps, ingress, statefulsets, daemonsets, jobs, pv, pvc.",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Specific resource name (optional). Required for describe and logs actions.",
			},
			"namespace": map[string]interface{}{
				"type":        "string",
				"description": "Namespace. Use 'all' for --all-namespaces. Omit for current namespace.",
			},
			"output": map[string]interface{}{
				"type":        "string",
				"description": "Output format: wide, yaml, json, name. Default: wide.",
				"enum":        []string{"wide", "yaml", "json", "name"},
				"default":     "wide",
			},
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "Label selector (e.g. 'app=nginx,tier=frontend'). Optional.",
			},
			"tail_lines": map[string]interface{}{
				"type":        "integer",
				"description": "Number of log lines for logs action (default: 50).",
				"default":     50,
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Target server name. Omit or use 'localhost' for local execution.",
			},
		},
	}
}

func (t *K8sQueryTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	action, _ := argString(args, "action", false)
	if action == "" {
		action = "get"
	}
	resource, _ := argString(args, "resource", false)
	name, _ := argString(args, "name", false)
	namespace, _ := argString(args, "namespace", false)
	output, _ := argString(args, "output", false)
	selector, _ := argString(args, "selector", false)
	tailLines := argInt(args, "tail_lines", 50)

	cmd := buildK8sCommand(action, resource, name, namespace, output, selector, tailLines)
	if cmd == "" {
		return "error: resource type is required for get/describe actions", nil
	}

	result, err := exec.Execute(ctx, cmd)
	if err != nil {
		return fmt.Sprintf("execution failed: %s", err), nil
	}
	if result.ExitCode != 0 && result.Stdout == "" {
		return fmt.Sprintf("kubectl error (exit %d):\n%s", result.ExitCode, result.Stderr), nil
	}
	out := strings.TrimSpace(result.Stdout)
	if result.Stderr != "" {
		out += "\n[stderr]\n" + strings.TrimSpace(result.Stderr)
	}
	return out, nil
}

func buildK8sCommand(action, resource, name, namespace, output, selector string, tailLines int) string {
	switch action {
	case "get":
		return buildK8sGet(resource, name, namespace, output, selector)
	case "describe":
		return buildK8sDescribe(resource, name, namespace)
	case "logs":
		return buildK8sLogs(name, namespace, tailLines)
	case "top":
		return buildK8sTop(resource, namespace)
	default:
		return ""
	}
}

func buildK8sGet(resource, name, namespace, output, selector string) string {
	if resource == "" {
		return ""
	}
	parts := []string{"kubectl", "get", resource}
	if name != "" {
		parts = append(parts, name)
	}
	parts = appendNamespace(parts, namespace)
	if output == "" {
		output = "wide"
	}
	parts = append(parts, "-o", output)
	if selector != "" {
		parts = append(parts, "-l", selector)
	}
	return strings.Join(parts, " ")
}

func buildK8sDescribe(resource, name, namespace string) string {
	if resource == "" || name == "" {
		return "kubectl get all" + nsFlag(namespace)
	}
	parts := []string{"kubectl", "describe", resource, name}
	parts = appendNamespace(parts, namespace)
	return strings.Join(parts, " ")
}

func buildK8sLogs(name, namespace string, tailLines int) string {
	if name == "" {
		return ""
	}
	parts := []string{"kubectl", "logs", name, fmt.Sprintf("--tail=%d", tailLines)}
	parts = appendNamespace(parts, namespace)
	return strings.Join(parts, " ")
}

func buildK8sTop(resource, namespace string) string {
	if resource == "" {
		resource = "pods"
	}
	parts := []string{"kubectl", "top", resource}
	parts = appendNamespace(parts, namespace)
	return strings.Join(parts, " ")
}

func appendNamespace(parts []string, namespace string) []string {
	if namespace == "all" {
		return append(parts, "--all-namespaces")
	}
	if namespace != "" {
		return append(parts, "-n", namespace)
	}
	return parts
}

func nsFlag(namespace string) string {
	if namespace == "all" {
		return " --all-namespaces"
	}
	if namespace != "" {
		return " -n " + namespace
	}
	return ""
}
