# File: system_risk.ps1
# Description: Fast-path 결정론 차단기 (Windows PowerShell 버전) — 위험 명령을 정규식으로 deny.
# Responsibility: PreToolUse command backend — stdin JSON -> stdout HookOutput JSON.

# stdin 전체를 읽는다.
$inputJson = $Input | Out-String
if ([string]::IsNullOrWhiteSpace($inputJson)) {
    Write-Output '{"decision":"allow"}'
    exit 0
}

try {
    $hookInput = $inputJson | ConvertFrom-Json
} catch {
    Write-Output '{"decision":"allow"}'
    exit 0
}

$tool = $hookInput.tool
$cmd = $hookInput.command

# tool 이 bash/shell_exec 가 아니면 즉시 allow.
if ($tool -notmatch '^(bash|shell_exec|shell|exec)$') {
    Write-Output '{"decision":"allow"}'
    exit 0
}

if ([string]::IsNullOrWhiteSpace($cmd)) {
    Write-Output '{"decision":"allow"}'
    exit 0
}

function Deny-Command($reason) {
    # JSON-safe escape
    $escapedReason = $reason -replace '\\', '\\' -replace '"', '\"'
    $json = '{"decision":"deny","reason":"{0}","systemMessage":"차단됨: {1}"}' -f $escapedReason, $escapedReason
    Write-Output $json
    exit 0
}

# 패턴 매칭 (-match).
# rm -rf 루트·시스템 디렉토리 차단
if ($cmd -match '(?i)(^|[^A-Za-z_])rm\s+(-[a-z]*[rRfF][a-z]*\s+)+(-[a-z]+\s+)*/\*?(\s|$)') {
    Deny-Command "rm -rf 루트 경로"
}

if ($cmd -match '(?i)(^|[^A-Za-z_])rm\s+(-[a-z]*[rRfF][a-z]*\s+)+(-[a-z]+\s+)*/(etc|var|usr|bin|sbin|boot|root|proc|sys|dev)') {
    Deny-Command "rm -rf 시스템 경로"
}

if ($cmd -match '(?i)(^|[^A-Za-z_])rm\s+(-[a-z]*[rRfF][a-z]*\s+)+(-[a-z]+\s+)*(~|\$HOME)(\s|$)') {
    Deny-Command "rm -rf 홈 디렉토리"
}

# dd of=/dev/sdX 또는 /dev/nvmeX
if ($cmd -match '(?i)dd\s+.*of=/dev/(sd[a-z]|nvme|hd[a-z]|xvd)') {
    Deny-Command "dd 디스크 블록 디바이스 쓰기"
}

# mkfs
if ($cmd -match '(?i)(^|[^A-Za-z_])mkfs(\.[a-z0-9]+)?\s+/dev/') {
    Deny-Command "mkfs 디바이스 포맷"
}

# fork bomb
if ($cmd -match ':\(\)\s*\{\s*:\s*\|\s*:') {
    Deny-Command "fork bomb 패턴"
}

# chmod -R 777 / 또는 시스템 경로
if ($cmd -match '(?i)chmod\s+(-R\s+)?777\s+(/|/\*|/etc|/var|/usr|/bin|/sbin|/root)(/\*)?(\s|$)') {
    Deny-Command "chmod 777 시스템 경로"
}

# chown -R ... / 또는 시스템 경로
if ($cmd -match '(?i)chown\s+-R\s+[^\s]+\s+(/|/etc|/var|/usr|/bin|/sbin|/root)(/\*)?(\s|$)') {
    Deny-Command "chown -R 시스템 경로"
}

# iptables -F / ufw disable
if ($cmd -match '(?i)(^|[^A-Za-z_])iptables\s+(-F|--flush)(\s|$)') {
    Deny-Command "iptables 전체 flush"
}
if ($cmd -match '(?i)(^|[^A-Za-z_])ufw\s+disable(\s|$)') {
    Deny-Command "ufw 방화벽 비활성화"
}

# > /dev/sda 직접 리다이렉션
if ($cmd -match '(?i)>\s*/dev/(sd[a-z]|nvme|hd[a-z]|xvd)') {
    Deny-Command "디스크 디바이스로 리다이렉션"
}

# 통과
Write-Output '{"decision":"allow"}'
