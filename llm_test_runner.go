//go:build tools
// +build tools

package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/config"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/llm"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

func main() {
	ctx := context.Background()

	// 1. Load Config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	configDir, _ := config.DefaultConfigDir()
	dbPath := filepath.Join(configDir, "infractl.db")

	// 2. Open Store
	st, err := store.NewSQLiteStore(ctx, dbPath)
	if err != nil {
		log.Fatalf("Failed to open store: %v", err)
	}
	defer st.Close()

	// 3. List Servers
	servers, err := st.List(ctx)
	if err != nil {
		log.Fatalf("Failed to list servers: %v", err)
	}

	fmt.Printf("--- Registered Servers (%d) ---\n", len(servers))
	for _, s := range servers {
		fmt.Printf("- %s (%s, %s)\n", s.Name, s.Host, s.OS)
	}

	// 4. Initialize LLM
	llmCfg := cfg.GeneralLLM()
	fmt.Printf("\n--- Using LLM: %s ---\n", llmCfg.Model)

	client := llm.NewOpenAIClient(llmCfg.Endpoint, llmCfg.Model, llmCfg.APIKey, time.Duration(llmCfg.Timeout)*time.Second)

	// 5. Ask LLM for scenarios
	prompt := `Windows 환경에서 인프라 관리 및 문제 해결 상황을 시뮬레이션하기 위한 다양한 쉘 명령어(PowerShell 또는 CMD) 15개를 생성해줘. 
각종 OS(Linux, Unix 등)에서 수행하던 작업들을 Windows에서 대응하는 명령어로 바꿔서 테스트해보고 싶어.
다음 카테고리를 포함해줘:
- 시스템 정보 및 OS 상세 확인 (CPU, 메모리, 커널/빌드 버전)
- 네트워크 상세 상태 (리스닝 포트, 라우팅 테이블, DNS 캐시)
- 프로세스 트리 및 서비스 상세 제어
- 파일 시스템 탐색 (용량 큰 파일 찾기, 특정 패턴 문자열 검색)
- 사용자 세션 및 그룹 권한 확인
- 환경 변수 및 레지스트리 설정 확인

응답은 명령어만 한 줄에 하나씩, 다른 설명 없이 텍스트로만 줘.`

	resp, err := client.Chat(ctx, []llm.Message{
		{Role: llm.RoleUser, Content: prompt},
	}, nil, nil)
	if err != nil {
		log.Fatalf("LLM Request failed: %v", err)
	}

	fmt.Println("\n--- LLM Generated Scenarios ---")
	fmt.Println(resp.Content)

	// 6. Execute Scenarios via ShellExecTool
	shellTool := &tools.ShellExecTool{}
	localExec := executor.NewLocalExecutor(60 * time.Second)

	lines := splitLines(resp.Content)
	fmt.Println("\n--- Executing Scenarios ---")
	for i, cmd := range lines {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" || strings.HasPrefix(cmd, "```") {
			continue
		}
		fmt.Printf("[%d] Running: %s\n", i+1, cmd)
		outcome, err := shellTool.Execute(ctx, map[string]interface{}{"command": cmd}, localExec)
		if err != nil {
			fmt.Printf("  Error: %v\n", err)
		} else {
			fmt.Printf("  Success: %v (ExitCode: %d)\n", outcome.Success, outcome.ExitCode)
			output := outcome.Content
			if len(output) > 500 {
				fmt.Printf("  Output: %s...\n", output[:500])
			} else {
				fmt.Printf("  Output: %s\n", output)
			}
		}
		fmt.Println("-------------------------------------------")
	}
}

func splitLines(s string) []string {
	var lines []string
	curr := ""
	for _, r := range s {
		if r == '\n' {
			lines = append(lines, curr)
			curr = ""
		} else {
			curr += string(r)
		}
	}
	if curr != "" {
		lines = append(lines, curr)
	}
	return lines
}
