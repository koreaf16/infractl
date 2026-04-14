package safety

import "regexp"

type installPattern struct {
	re     *regexp.Regexp
	reason string
}

var installContextPatterns = []installPattern{
	// 시스템 패키지 관리자
	{
		re:     regexp.MustCompile(`(?i)\b(apt|apt-get|yum|dnf|zypper|brew)\b[^\n]*\binstall\b`),
		reason: "package installation",
	},
	{
		re:     regexp.MustCompile(`(?i)\bpacman\s+-[A-Za-z]*S`),
		reason: "package installation",
	},
	{
		re:     regexp.MustCompile(`(?i)\b(rpm|dpkg)\b[^\n]*(--install|\s-[A-Za-z]*i[A-Za-z]*)`),
		reason: "package installation",
	},
	// 개발 도구 설치
	{
		re:     regexp.MustCompile(`(?i)\b(pip|pip3|npm|gem|cargo)\b[^\n]*\binstall\b|\bgo\s+install\b`),
		reason: "tool or runtime installation",
	},
	// 빌드 설치
	{
		re:     regexp.MustCompile(`(?i)\b(make|cmake)\b[^\n]*\binstall\b`),
		reason: "build install step",
	},
	// 전용 설치 프로그램
	{
		re:     regexp.MustCompile(`(?i)\b(runinstaller|dbca|netca)\b`),
		reason: "installer execution",
	},
	// 소프트웨어 환경 변수 설정
	{
		re:     regexp.MustCompile(`(?i)\b(ORACLE_BASE|ORACLE_HOME|ORACLE_SID|JAVA_HOME|CATALINA_HOME|KAFKA_HOME|LD_LIBRARY_PATH)\b`),
		reason: "software environment setup",
	},
	// 컨테이너/오케스트레이션
	{
		re:     regexp.MustCompile(`(?i)\bdocker\s+(pull|compose\s+up)\b`),
		reason: "container image/stack setup",
	},
	{
		re:     regexp.MustCompile(`(?i)\bhelm\s+(install|upgrade\s+--install)\b`),
		reason: "kubernetes chart installation",
	},
	// 리눅스 범용 패키지
	{
		re:     regexp.MustCompile(`(?i)\b(snap|flatpak)\b[^\n]*\binstall\b`),
		reason: "universal package installation",
	},
	// Windows 패키지
	{
		re:     regexp.MustCompile(`(?i)\b(choco|winget)\b[^\n]*\binstall\b|\bmsiexec\b`),
		reason: "windows package installation",
	},
	// 스크립트/빌드 기반 설치
	{
		re:     regexp.MustCompile(`(?i)\./(?:setup|install|configure)\b`),
		reason: "script-based installation",
	},
}

// InstallContextReason reports why a shell command should carry an install/setup plan.
func InstallContextReason(command string) string {
	for _, p := range installContextPatterns {
		if p.re.MatchString(command) {
			return p.reason
		}
	}
	return ""
}
