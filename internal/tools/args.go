// Package tools
// File: args.go
// Description: 도구 인자 맵에서 타입별로 값을 추출하는 헬퍼 함수
// Responsibility: LLM이 생성한 JSON 인자의 안전한 타입 추출

package tools

import "fmt"

// argString은 args 맵에서 key에 해당하는 문자열 값을 반환한다.
// required가 true이고 값이 없으면 error를 반환한다.
func argString(args map[string]interface{}, key string, required bool) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		if required {
			return "", fmt.Errorf("required argument missing: %s", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v), nil
	}
	return s, nil
}

// argInt는 args 맵에서 key에 해당하는 정수 값을 반환한다.
// JSON 숫자는 float64로 파싱되므로 변환을 처리한다.
func argInt(args map[string]interface{}, key string, defaultVal int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return defaultVal
}

// ExtractTarget은 args 맵에서 "target" 값을 추출하고 맵에서 삭제한다.
// 에이전트 루프에서 호출되며, 도구 자체는 target을 처리하지 않는다.
func ExtractTarget(args map[string]interface{}) string {
	v, ok := args["target"]
	if !ok || v == nil {
		return ""
	}
	delete(args, "target")
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// argBool은 args 맵에서 key에 해당하는 불린 값을 반환한다.
func argBool(args map[string]interface{}, key string, defaultVal bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return defaultVal
	}
	b, ok := v.(bool)
	if !ok {
		return defaultVal
	}
	return b
}
