// Package agent
// File: equivalence_test.go
// Description: hook 기반 검증의 동등성 테스트 — testdata/equivalence/*.yaml 케이스 일괄 실행
// Responsibility: system_risk.sh command backend + ComputeMetadata 의 분류 결과를 50+ 케이스로 회귀 검증

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yourorg/infractl/internal/agent/query"
	"gopkg.in/yaml.v3"
)

// ─── 케이스 YAML 구조 ─────────────────────────────────────────────────────────

type equivCaseFile struct {
	Cases []equivCase `yaml:"cases"`
}

type equivCase struct {
	ID             string `yaml:"id"`
	Tool           string `yaml:"tool"`
	Command        string `yaml:"command"`
	ExpectDeny     bool   `yaml:"expect_deny"`
	ExpectKeyword  string `yaml:"expect_keyword"`  // deny reason 에 포함되어야 하는 키워드 (선택)
	ExpectReadonly bool   `yaml:"expect_readonly"` // shell_readonly.yaml 전용
}

// ─── 헬퍼 ─────────────────────────────────────────────────────────────────────

// scriptPath 는 system_risk.sh 의 절대 경로를 반환한다.
func scriptPath(t *testing.T) string {
	t.Helper()
	// 이 파일은 internal/agent/ 에 있으므로 프로젝트 루트는 두 단계 위.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	sh := filepath.Join(root, "internal", "hooks", "builtins", "system_risk.sh")
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("system_risk.sh not found (%v) — skipping sh-dependent tests", err)
	}
	return sh
}

// runScript 는 system_risk.sh 에 hookInput JSON 을 전달하고 HookOutput 을 반환한다.
func runScript(t *testing.T, sh string, tool, command string) (decision string, reason string) {
	t.Helper()
	input := map[string]any{
		"event": "PreToolUse",
		"tool":  tool,
	}
	if command != "" {
		input["input"] = map[string]any{"command": command}
	}
	// json.Marshal 은 기본적으로 <>&를 \u00xx 로 이스케이프한다. 셸 스크립트가 원본 문자를 보도록 비활성화.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(input); err != nil {
		t.Fatalf("encode hook input: %v", err)
	}
	jsonB := buf.Bytes()

	// Windows: git-bash sh 전제
	cmd := exec.CommandContext(context.Background(), "sh", sh)
	cmd.Stdin = bytes.NewReader(jsonB)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("script %s failed: %v", sh, err)
	}

	var result struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if jerr := json.Unmarshal(bytes.TrimSpace(out), &result); jerr != nil {
		t.Fatalf("parse script output %q: %v", string(out), jerr)
	}
	return result.Decision, result.Reason
}

// loadCases 는 testdata/equivalence/<name>.yaml 을 로드한다.
func loadCases(t *testing.T, name string) []equivCase {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	path := filepath.Join(root, "testdata", "equivalence", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cf equivCaseFile
	if err := yaml.Unmarshal(b, &cf); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cf.Cases
}

// ─── 테스트: system_risk.sh deny 케이스 (20) ─────────────────────────────────

func TestEquivalence_SystemRisk_Deny(t *testing.T) {
	sh := scriptPath(t)
	cases := loadCases(t, "system_risk_deny.yaml")

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("%s/%s", tc.ID, tc.Tool), func(t *testing.T) {
			decision, reason := runScript(t, sh, tc.Tool, tc.Command)
			if tc.ExpectDeny && decision != "deny" {
				t.Errorf("[%s] want deny, got %q  (cmd=%q)", tc.ID, decision, tc.Command)
			}
			if !tc.ExpectDeny && decision == "deny" {
				t.Errorf("[%s] want allow, got deny (reason=%q, cmd=%q)", tc.ID, reason, tc.Command)
			}
			if tc.ExpectKeyword != "" && !strings.Contains(reason, tc.ExpectKeyword) {
				t.Logf("[%s] WARN: reason %q does not contain expected keyword %q", tc.ID, reason, tc.ExpectKeyword)
			}
		})
	}
}

// ─── 테스트: system_risk.sh allow 케이스 (15) ─────────────────────────────────

func TestEquivalence_SystemRisk_Allow(t *testing.T) {
	sh := scriptPath(t)
	cases := loadCases(t, "system_risk_allow.yaml")

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("%s/%s", tc.ID, tc.Tool), func(t *testing.T) {
			decision, reason := runScript(t, sh, tc.Tool, tc.Command)
			if tc.ExpectDeny && decision != "deny" {
				t.Errorf("[%s] want deny, got %q (cmd=%q)", tc.ID, decision, tc.Command)
			}
			if !tc.ExpectDeny && decision == "deny" {
				t.Errorf("[%s] want allow, got deny (reason=%q, cmd=%q)", tc.ID, reason, tc.Command)
			}
		})
	}
}

// ─── 테스트: ComputeMetadata read_only 분류 (10) ─────────────────────────────

func TestEquivalence_ShellReadonly(t *testing.T) {
	cases := loadCases(t, "shell_readonly.yaml")

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("%s/%s", tc.ID, tc.Tool), func(t *testing.T) {
			args := map[string]any{}
			if tc.Command != "" {
				args["command"] = tc.Command
			}
			md := query.ComputeMetadata(tc.Tool, args)
			if tc.ExpectReadonly && !md.ReadOnly {
				t.Errorf("[%s] want read_only=true, got %+v (cmd=%q)", tc.ID, md, tc.Command)
			}
			if !tc.ExpectReadonly && md.ReadOnly {
				t.Errorf("[%s] want read_only=false, got %+v (cmd=%q)", tc.ID, md, tc.Command)
			}
		})
	}
}

// ─── 테스트: 비 bash 도구 pass-through (10) ─────────────────────────────────

func TestEquivalence_PassThrough(t *testing.T) {
	sh := scriptPath(t)
	cases := loadCases(t, "pass_through.yaml")

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("%s/%s", tc.ID, tc.Tool), func(t *testing.T) {
			decision, reason := runScript(t, sh, tc.Tool, tc.Command)
			if tc.ExpectDeny && decision != "deny" {
				t.Errorf("[%s] want deny, got %q (tool=%s cmd=%q)", tc.ID, decision, tc.Tool, tc.Command)
			}
			if !tc.ExpectDeny && decision == "deny" {
				t.Errorf("[%s] want allow, got deny (reason=%q, tool=%s cmd=%q)", tc.ID, reason, tc.Tool, tc.Command)
			}
		})
	}
}
