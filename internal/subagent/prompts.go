// Package subagent
// File: prompts.go
// Description: 서브에이전트 타입별 시스템 프롬프트 및 허용 도구 목록 정의
// Responsibility: AgentType별 전문화된 시스템 프롬프트와 허용 도구 필터 제공

package subagent

import "strings"

// agentPrompts는 AgentType별 시스템 프롬프트이다.
var agentPrompts = map[AgentType]string{
	AgentTypeDB: `당신은 데이터베이스 전문 분석 에이전트입니다.
Oracle, MySQL, PostgreSQL 등 RDBMS의 상태, 성능, 용량을 분석합니다.
주어진 서버의 DB 상태를 조사하고 문제점과 개선 사항을 보고하세요.
반드시 DB 관련 도구(oracle_*, mysql_*, postgresql_* 등)를 사용하여 실제 데이터를 수집하세요.`,

	AgentTypeOS: `당신은 운영체제 전문 분석 에이전트입니다.
서버의 프로세스, CPU/메모리 사용량, 파일시스템, 네트워크 상태를 분석합니다.
shell_exec, process_list, network_info, file_read 도구를 사용하여 실제 상태를 수집하세요.
주요 점검 항목: 좀비 프로세스, 디스크 사용량, 오래된 로그, 열린 포트.`,

	AgentTypeWAS: `당신은 WAS(Web Application Server) 전문 분석 에이전트입니다.
Tomcat, WebLogic 등 애플리케이션 서버의 상태, 쓰레드 풀, 커넥션 풀을 분석합니다.
tomcat_*, weblogic_* 도구를 사용하여 실제 서버 상태를 수집하고 보고하세요.
주요 점검: 배포된 앱 목록, 스레드 사용량, 메모리 힙 상태.`,

	AgentTypeSecurity: `당신은 보안 분석 전문 에이전트입니다.
서버의 보안 설정, 열린 포트, 실행 중인 프로세스, 의심스러운 파일을 분석합니다.
shell_exec, process_list, network_info, file_read 도구를 사용하여 보안 취약점을 찾으세요.
주요 점검: 불필요한 포트 개방, 권한 설정 이상, 비정상 프로세스.`,
}

// allowedToolPrefixes는 AgentType별 허용 도구 이름 또는 접두사 목록이다.
// 빈 슬라이스면 모든 도구 허용.
var allowedToolPrefixes = map[AgentType][]string{
	AgentTypeDB:       {"oracle_", "mysql_", "postgresql_", "pg_"},
	AgentTypeOS:       {"shell_exec", "file_read", "file_write", "process_list", "network_info"},
	AgentTypeWAS:      {"tomcat_", "weblogic_", "shell_exec"},
	AgentTypeSecurity: {"shell_exec", "process_list", "network_info", "file_read"},
}

// SystemPrompt는 AgentType에 맞는 시스템 프롬프트를 반환한다.
func SystemPrompt(t AgentType) string {
	if p, ok := agentPrompts[t]; ok {
		return p
	}
	return "당신은 인프라 분석 에이전트입니다. 주어진 서버를 분석하고 보고하세요."
}

// ToolAllowed는 도구명이 AgentType의 허용 목록에 포함되는지 확인한다.
func ToolAllowed(t AgentType, toolName string) bool {
	prefixes, ok := allowedToolPrefixes[t]
	if !ok || len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if toolName == p || strings.HasPrefix(toolName, p) {
			return true
		}
	}
	return false
}
