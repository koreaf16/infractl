// Package oracle
// File: queries.go
// Description: Oracle 도구에서 사용하는 SQL 쿼리 및 쉘 명령 템플릿
// Responsibility: sqlplus로 실행할 쿼리와 명령을 문자열 상수로 관리

package oracle

import "fmt"

// buildConnStr은 sqlplus 접속 문자열을 생성한다.
// host가 있으면 Easy Connect 방식, 없으면 TNSNAMES 방식이다.
// creds.OSAuth가 true이면 "/ as sysdba" OS 인증 문자열을 반환한다.
func buildConnStr(user, pass, role, host string, port int, sid string, osAuth bool) string {
	if osAuth {
		// OS 인증: 사용자 이름/패스워드 없이 sysdba로 접속
		return "/ as sysdba"
	}
	connStr := user + "/" + pass
	if host != "" {
		if port > 0 {
			connStr += fmt.Sprintf("@%s:%d/%s", host, port, sid)
		} else {
			connStr += fmt.Sprintf("@%s/%s", host, sid)
		}
	} else if sid != "" {
		connStr += "@" + sid
	}
	if role == "sysdba" {
		connStr += " as sysdba"
	}
	return connStr
}

// buildOSAuthProbeCmd는 Oracle OS 인증 가능 여부를 확인하는 명령을 생성한다.
// ORACLE_SID 환경변수를 설정한 후 "/ as sysdba"로 접속하여 OS_AUTH_OK를 출력한다.
func buildOSAuthProbeCmd(sid string) string {
	return fmt.Sprintf(`ORACLE_SID=%s sqlplus -S '/ as sysdba' <<'SQLEOF'
SET FEEDBACK OFF
SET PAGESIZE 0
SELECT 'OS_AUTH_OK' FROM dual;
EXIT;
SQLEOF`, sid)
}

// buildSQLPlusCmd은 SQL을 실행하는 sqlplus 명령을 생성한다.
func buildSQLPlusCmd(connStr, sql string) string {
	return fmt.Sprintf(`sqlplus -S '%s' <<'SQLEOF'
SET LINESIZE 200
SET PAGESIZE 50
SET FEEDBACK OFF
%s
EXIT;
SQLEOF`, connStr, sql)
}

// tablespaceQuery는 테이블스페이스 사용량 조회 쿼리이다.
const tablespaceQuery = `
SELECT
  d.tablespace_name,
  ROUND(d.allocated_space/1024/1024,1) AS allocated_mb,
  ROUND(d.used_space/1024/1024,1) AS used_mb,
  ROUND((d.allocated_space-d.used_space)/1024/1024,1) AS free_mb,
  ROUND(d.used_percent,1) AS used_pct
FROM dba_tablespace_usage_metrics d
ORDER BY d.used_percent DESC;`

// sessionsQuery는 활성 세션 조회 쿼리이다.
const sessionsQuery = `
SELECT
  s.sid, s.serial#, s.username, s.status, s.machine,
  s.program, s.sql_id, s.event
FROM v$session s
WHERE s.type = 'USER'
ORDER BY s.status, s.username;`

// locksQuery는 락 정보 조회 쿼리이다.
const locksQuery = `
SELECT
  s.sid, s.serial#, s.username, l.type, l.lmode, l.request,
  l.block, s.status, s.machine
FROM v$lock l JOIN v$session s ON l.sid = s.sid
WHERE l.block = 1 OR l.request > 0
ORDER BY l.block DESC;`

// topSQLQuery는 부하 SQL 조회 쿼리이다.
const topSQLQuery = `
SELECT * FROM (
  SELECT sql_id, executions,
         ROUND(elapsed_time/1000000,2) AS elapsed_sec,
         ROUND(elapsed_time/GREATEST(executions,1)/1000000,4) AS avg_elapsed,
         SUBSTR(sql_text,1,100) AS sql_text
  FROM v$sql
  WHERE executions > 0
  ORDER BY elapsed_time DESC
) WHERE ROWNUM <= 20;`

// alertLogCmd는 alert 로그 마지막 N줄을 읽는 명령이다.
func alertLogCmd(oracleHome, sid string, lines int) string {
	return fmt.Sprintf(
		`find %s/diag/rdbms -name 'alert_%s.log' 2>/dev/null | head -1 | xargs -I{} tail -n %d {}`,
		oracleHome, sid, lines,
	)
}
