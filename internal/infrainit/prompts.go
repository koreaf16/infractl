// Package infrainit
// File: prompts.go
// Description: 위저드용 터미널 I/O 헬퍼 함수 모음
// Responsibility: 표준 입력으로부터 텍스트, Y/N, 번호 선택을 읽는 함수 제공

package infrainit

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// promptText는 라벨을 출력하고 한 줄을 읽어 반환한다.
// 빈 입력이면 defaultVal을 반환한다.
func promptText(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

// promptSecret는 라벨을 출력하고 에코 없이 한 줄을 읽어 반환한다.
// API Key 등 민감한 값 입력에 사용한다.
func promptSecret(label string) string {
	fmt.Printf("  %s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println() // ReadPassword는 개행을 출력하지 않으므로 직접 출력
	if err != nil {
		// 터미널이 아닌 환경(파이프, 테스트)에서는 일반 readline으로 폴백
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		return strings.TrimSpace(input)
	}
	return strings.TrimSpace(string(b))
}

// promptYN은 Y/N 질문을 출력하고 bool을 반환한다.
// defaultYes=true이면 빈 입력을 Yes로 처리한다.
func promptYN(reader *bufio.Reader, question string, defaultYes bool) bool {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	fmt.Printf("  %s (%s): ", question, hint)

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return defaultYes
	}
	return input == "y" || input == "yes"
}

// promptSelect는 목록을 번호로 출력하고 선택된 0-based 인덱스를 반환한다.
// 유효하지 않은 번호 입력 시 재시도한다.
func promptSelect(reader *bufio.Reader, options []string) int {
	for i, opt := range options {
		fmt.Printf("    %d) %s\n", i+1, opt)
	}
	for {
		fmt.Printf("  번호 입력 [1]: ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			return 0
		}
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(options) {
			fmt.Printf("  1~%d 사이의 번호를 입력하세요.\n", len(options))
			continue
		}
		return n - 1
	}
}

// printSectionHeader는 단계 헤더를 출력한다.
func printSectionHeader(step, title string) {
	fmt.Printf("\n[%s] %s\n", step, title)
}

// printSuccess는 성공 메시지를 출력한다.
func printSuccess(msg string) {
	fmt.Printf("\n✓ %s\n", msg)
}
