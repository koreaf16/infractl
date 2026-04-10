// Package rag
// File: query_builders.go
// Description: DB별 벡터 검색 SQL 및 CLI 명령 빌더
// Responsibility: Oracle/MySQL/PostgreSQL 벡터 검색 쿼리를 DB CLI 실행 형태로 변환

package rag

import (
	"fmt"
	"strings"
)

// buildVectorSearchCmd는 db_type에 따라 벡터 검색 CLI 명령을 반환한다.
// 벡터는 임시 파일(/tmp/rag_vec_query.sql)에 기록하여 셸 인자 길이 제한을 우회한다.
func buildVectorSearchCmd(dbType, dbName, host string, port int, username, password, role,
	tableName, textCol, vecCol, resultCols, vecStr string, topK int) string {
	switch dbType {
	case "oracle":
		return buildOracleVectorCmd(dbName, host, port, username, password, role,
			tableName, vecCol, resultCols, vecStr, topK)
	case "mysql":
		return buildMySQLVectorCmd(dbName, host, port, username, password,
			tableName, textCol, vecCol, resultCols, vecStr, topK)
	case "postgresql":
		return buildPGVectorCmd(dbName, host, port, username, password,
			tableName, vecCol, resultCols, vecStr, topK)
	default:
		return ""
	}
}

// buildOracleVectorCmd는 Oracle VECTOR_DISTANCE를 사용하는 명령을 생성한다.
// 벡터 리터럴이 길어 임시 SQL 파일 방식을 사용한다.
func buildOracleVectorCmd(sid, host string, port int, user, pass, role,
	table, vecCol, resultCols, vecStr string, topK int) string {
	if port == 0 {
		port = 1521
	}
	connStr := fmt.Sprintf("%s/%s@%s:%d/%s", user, pass, host, port, sid)
	if role == "sysdba" {
		connStr += " as sysdba"
	}

	// Oracle VECTOR 리터럴 형식: VECTOR('[0.1,0.2,...]', dim, FLOAT32)
	sqlContent := fmt.Sprintf(
		"SELECT %s FROM %s ORDER BY VECTOR_DISTANCE(%s, VECTOR('%s', %d, FLOAT32), COSINE) FETCH FIRST %d ROWS ONLY;\nEXIT;\n",
		resultCols, table, vecCol, vecStr, len(strings.Split(vecStr, ",")), topK,
	)

	return fmt.Sprintf(
		`echo %s > /tmp/rag_vec_q.sql && sqlplus -L -S '%s' @/tmp/rag_vec_q.sql && rm -f /tmp/rag_vec_q.sql`,
		shellQuote(sqlContent), connStr,
	)
}

// buildMySQLVectorCmd는 MySQL 9.0+ STRING_TO_VECTOR를 사용하는 명령을 생성한다.
func buildMySQLVectorCmd(dbName, host string, port int, user, pass,
	table, textCol, vecCol, resultCols, vecStr string, topK int) string {
	if port == 0 {
		port = 3306
	}
	sqlContent := fmt.Sprintf(
		"SELECT %s, DISTANCE(%s, STRING_TO_VECTOR('%s'), 'cosine') AS _dist FROM %s ORDER BY _dist LIMIT %d;\n",
		resultCols, vecCol, vecStr, table, topK,
	)
	return fmt.Sprintf(
		`echo %s > /tmp/rag_vec_q.sql && MYSQL_PWD='%s' mysql -u '%s' -h '%s' -P %d -D '%s' --batch --silent < /tmp/rag_vec_q.sql && rm -f /tmp/rag_vec_q.sql`,
		shellQuote(sqlContent), pass, user, host, port, dbName,
	)
}

// buildPGVectorCmd는 pgvector <=> 연산자를 사용하는 명령을 생성한다.
func buildPGVectorCmd(dbName, host string, port int, user, pass,
	table, vecCol, resultCols, vecStr string, topK int) string {
	if port == 0 {
		port = 5432
	}
	// pgvector 리터럴 형식: '[0.1,0.2,...]'::vector
	sqlContent := fmt.Sprintf(
		"SELECT %s FROM %s ORDER BY %s <=> '[%s]'::vector LIMIT %d;\n",
		resultCols, table, vecCol, vecStr, topK,
	)
	return fmt.Sprintf(
		`echo %s > /tmp/rag_vec_q.sql && PGPASSWORD='%s' psql -U '%s' -h '%s' -p %d -d '%s' -f /tmp/rag_vec_q.sql && rm -f /tmp/rag_vec_q.sql`,
		shellQuote(sqlContent), pass, user, host, port, dbName,
	)
}

// formatVectorStr은 []float32를 콤마 구분 문자열로 변환한다.
func formatVectorStr(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%.6f", f)
	}
	return strings.Join(parts, ",")
}

// shellQuote는 single-quote로 문자열을 감싼다. 내부 single-quote는 이스케이프한다.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
