// Package safety
// File: sql_classifier.go
// Description: SQL 위험도 분류 및 백업 SQL 생성
// Responsibility: SQL 문의 위험도를 분석하여 백업 쿼리와 공간 확인 쿼리를 제공

package safety

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/tools"
)

// SQLRisk는 SQL 문의 위험도 분석 결과이다.
type SQLRisk struct {
	Level          tools.RiskLevel
	RequiresBackup bool
	TableName      string // DROP/TRUNCATE/DELETE에서 추출한 테이블명
	BackupSQL      string // "CREATE TABLE tbl_BAK_20260409_143022 AS SELECT * FROM tbl"
	BackupTable    string // 백업 테이블명
}

// ClassifySQL은 SQL 문을 분석하여 위험도와 백업 정보를 반환한다.
func ClassifySQL(sql string) SQLRisk {
	normalized := strings.ToUpper(strings.TrimSpace(sql))
	// 여러 공백을 단일 공백으로 정규화
	for strings.Contains(normalized, "  ") {
		normalized = strings.ReplaceAll(normalized, "  ", " ")
	}

	first := firstKeyword(normalized)

	switch first {
	case "SELECT", "SHOW", "EXPLAIN", "DESC", "DESCRIBE", "WITH":
		return SQLRisk{Level: tools.RiskNone}

	case "INSERT", "UPDATE", "MERGE":
		return SQLRisk{Level: tools.RiskMedium}

	case "ALTER":
		if strings.Contains(normalized, "DROP COLUMN") {
			return SQLRisk{Level: tools.RiskHigh, RequiresBackup: true, TableName: extractAlterTableName(normalized)}
		}
		return SQLRisk{Level: tools.RiskMedium}

	case "DELETE":
		tableName := extractDeleteTableName(normalized)
		hasWhere := strings.Contains(normalized, " WHERE ")
		level := tools.RiskMedium
		if !hasWhere {
			level = tools.RiskHigh
		}
		backupTable, backupSQL := buildBackupSQL(tableName)
		return SQLRisk{
			Level:          level,
			RequiresBackup: true,
			TableName:      tableName,
			BackupTable:    backupTable,
			BackupSQL:      backupSQL,
		}

	case "TRUNCATE":
		tableName := extractTruncateTableName(normalized)
		backupTable, backupSQL := buildBackupSQL(tableName)
		return SQLRisk{
			Level:          tools.RiskHigh,
			RequiresBackup: true,
			TableName:      tableName,
			BackupTable:    backupTable,
			BackupSQL:      backupSQL,
		}

	case "DROP":
		second := secondKeyword(normalized)
		if second == "TABLE" {
			tableName := extractDropTableName(normalized)
			backupTable, backupSQL := buildBackupSQL(tableName)
			return SQLRisk{
				Level:          tools.RiskHigh,
				RequiresBackup: true,
				TableName:      tableName,
				BackupTable:    backupTable,
				BackupSQL:      backupSQL,
			}
		}
		// DROP INDEX/VIEW/SEQUENCE 등
		return SQLRisk{Level: tools.RiskMedium}
	}

	return SQLRisk{Level: tools.RiskLow}
}

// SpaceCheckSQL은 DB 타입과 테이블명으로 공간 사전 확인 쿼리를 반환한다.
// 반환 쿼리는 table_mb, free_mb 두 컬럼을 포함한 단일 행을 반환해야 한다.
func SpaceCheckSQL(dbType, tableName string) string {
	upper := strings.ToUpper(tableName)
	switch strings.ToLower(dbType) {
	case "oracle":
		return fmt.Sprintf(`SELECT ROUND(NVL(s.bytes,0)/1048576,1) AS table_mb, ROUND(NVL(f.free_mb,0),1) AS free_mb FROM (SELECT SUM(bytes) AS bytes FROM dba_segments WHERE segment_name = '%s') s, (SELECT SUM(bytes)/1048576 AS free_mb FROM dba_free_space WHERE tablespace_name = (SELECT tablespace_name FROM dba_segments WHERE segment_name = '%s' AND ROWNUM=1)) f`, upper, upper)
	case "mysql":
		return fmt.Sprintf(`SELECT ROUND(data_length/1048576,1) AS table_mb, ROUND(data_free/1048576,1) AS free_mb FROM information_schema.TABLES WHERE table_schema = DATABASE() AND table_name = '%s'`, tableName)
	case "postgresql", "postgres":
		return fmt.Sprintf(`SELECT ROUND(pg_total_relation_size('%s')/1048576.0,1) AS table_mb, ROUND(pg_available_disk_space()/1048576.0,1) AS free_mb`, tableName)
	default:
		return ""
	}
}

// ParseSpaceCheckResult는 공간 확인 쿼리 결과를 파싱하여 (tableMB, freeMB, ok)를 반환한다.
func ParseSpaceCheckResult(output string) (tableMB, freeMB float64, ok bool) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "TABLE_MB") || strings.HasPrefix(line, "table_mb") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			t, err1 := strconv.ParseFloat(strings.ReplaceAll(parts[0], ",", "."), 64)
			f, err2 := strconv.ParseFloat(strings.ReplaceAll(parts[1], ",", "."), 64)
			if err1 == nil && err2 == nil {
				return t, f, true
			}
		}
	}
	return 0, 0, false
}

// FileSpaceCheckCmd는 파일 백업 전 OS 레벨 공간 확인 명령을 반환한다.
// 결과 stdout이 "INSUFFICIENT:needed_kb:available_kb" 또는 "OK:available_kb" 형식이다.
func FileSpaceCheckCmd(filePath string) string {
	return fmt.Sprintf(
		`available=$(df -k "$(dirname '%s')" 2>/dev/null | awk 'NR==2{print $4}'); needed=$(du -k '%s' 2>/dev/null | cut -f1 || echo 0); [ -z "$available" ] && echo "OK:0" || { [ "$needed" -gt "$available" ] && echo "INSUFFICIENT:${needed}:${available}" || echo "OK:${available}"; }`,
		filePath, filePath,
	)
}

// IsDiskSpaceError는 명령 출력에서 디스크 공간 부족 오류를 감지한다.
func IsDiskSpaceError(output string) bool {
	lower := strings.ToLower(output)
	keywords := []string{"no space left", "disk full", "not enough space", "insufficient space", "enospc", "no space available"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// buildBackupSQL은 백업 테이블명과 백업 SQL을 생성한다.
func buildBackupSQL(tableName string) (backupTable, backupSQL string) {
	if tableName == "" {
		return "", ""
	}
	ts := time.Now().Format("20060102_150405")
	backupTable = fmt.Sprintf("%s_BAK_%s", strings.ToUpper(tableName), ts)
	backupSQL = fmt.Sprintf("CREATE TABLE %s AS SELECT * FROM %s", backupTable, strings.ToUpper(tableName))
	return backupTable, backupSQL
}

func firstKeyword(normalized string) string {
	parts := strings.Fields(normalized)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func secondKeyword(normalized string) string {
	parts := strings.Fields(normalized)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func extractDropTableName(normalized string) string {
	// DROP TABLE [IF EXISTS] tablename
	parts := strings.Fields(normalized)
	for i, p := range parts {
		if p == "TABLE" {
			if i+1 < len(parts) && parts[i+1] == "IF" && i+3 < len(parts) {
				return cleanTableName(parts[i+3])
			}
			if i+1 < len(parts) {
				return cleanTableName(parts[i+1])
			}
		}
	}
	return ""
}

func extractTruncateTableName(normalized string) string {
	// TRUNCATE [TABLE] tablename
	parts := strings.Fields(normalized)
	for i, p := range parts {
		if p == "TRUNCATE" {
			if i+1 < len(parts) {
				next := parts[i+1]
				if next == "TABLE" && i+2 < len(parts) {
					return cleanTableName(parts[i+2])
				}
				return cleanTableName(next)
			}
		}
	}
	return ""
}

func extractDeleteTableName(normalized string) string {
	// DELETE FROM tablename [WHERE ...]
	parts := strings.Fields(normalized)
	for i, p := range parts {
		if p == "FROM" && i+1 < len(parts) {
			return cleanTableName(parts[i+1])
		}
	}
	return ""
}

func extractAlterTableName(normalized string) string {
	// ALTER TABLE tablename ...
	parts := strings.Fields(normalized)
	for i, p := range parts {
		if p == "TABLE" && i+1 < len(parts) {
			return cleanTableName(parts[i+1])
		}
	}
	return ""
}

func cleanTableName(name string) string {
	// 스키마.테이블 형식에서 테이블명만 추출, 따옴표 제거
	name = strings.Trim(name, `"'` + "`")
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	// WHERE, SET, ; 등 후속 토큰이 붙은 경우 제거
	for _, stop := range []string{"WHERE", "SET", ";"} {
		if strings.HasSuffix(strings.ToUpper(name), stop) {
			name = name[:len(name)-len(stop)]
		}
	}
	return strings.TrimRight(name, ";,")
}
