// Package oracle
// File: connector.go
// Description: Oracle DB 커넥터 — sqlplus 기반 6개 도구 세트 생성
// Responsibility: Oracle 접속 테스트 및 query/tablespace/sessions/locks/alert_log/top_sql 도구 생성

package oracle

import (
	"context"
	"fmt"
	"strings"

	conn "github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/pipeline"
	"github.com/yourorg/infractl/internal/safety"
	"github.com/yourorg/infractl/internal/tools"
)

// OracleConnector는 Oracle DB 전용 커넥터이다.
type OracleConnector struct {
	info   conn.ServiceInfo
	creds  conn.Credentials
	status conn.ConnectorStatus
	names  []string
}

// New는 OracleConnector를 생성한다.
func New() *OracleConnector {
	return &OracleConnector{status: conn.StatusDisconnected}
}

func (c *OracleConnector) ServiceType() string        { return "oracle" }
func (c *OracleConnector) Status() conn.ConnectorStatus { return c.status }
func (c *OracleConnector) ToolNames() []string          { return c.names }

// GenerateTools는 Oracle 접속 정보로 6개 도구를 생성한다.
func (c *OracleConnector) GenerateTools(info conn.ServiceInfo, creds conn.Credentials) []tools.Tool {
	c.info = info
	c.creds = creds
	c.status = conn.StatusConnected

	sid := info.Name
	if sid == "" {
		sid = "ORCL"
	}
	prefix := fmt.Sprintf("oracle_%s", strings.ToLower(sid))

	connStr := buildConnStr(creds.Username, creds.Password, creds.Role,
		info.Details["host"], info.Port, sid, creds.OSAuth)

	oracleHome := info.Details["oracle_home"]

	toolList := []tools.Tool{
		c.makeQueryTool(prefix, connStr),
		c.makeTablespaceTool(prefix, connStr),
		c.makeSessionsTool(prefix, connStr),
		c.makeLocksTool(prefix, connStr),
		c.makeAlertLogTool(prefix, oracleHome, sid),
		c.makeTopSQLTool(prefix, connStr),
	}

	c.names = make([]string, len(toolList))
	for i, t := range toolList {
		c.names[i] = t.Name()
	}
	return toolList
}

func (c *OracleConnector) makeQueryTool(prefix, connStr string) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".query",
		fmt.Sprintf("Oracle(%s) SQL 실행. SELECT/DML 모두 가능. 결과를 텍스트로 반환.", prefix),
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"sql":    map[string]interface{}{"type": "string", "description": "실행할 SQL 문"},
				"target": targetParam(),
				"skip_backup": map[string]interface{}{
					"type":        "boolean",
					"description": "true이면 사전 백업을 건너뜁니다. 공간 부족 등 백업 불가 확인 후에만 사용.",
				},
			},
			"required": []string{"sql"},
		},
		tools.RiskLow, false, &c.status,
		func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
			sql, _ := args["sql"].(string)
			if sql == "" {
				return "sql 인자가 필요합니다", nil
			}
			skipBackup, _ := args["skip_backup"].(bool)

			classification := safety.ClassifySQL(sql)

			if classification.RequiresBackup && !skipBackup && classification.TableName != "" {
				// 1. 공간 사전 확인
				spaceSQL := safety.SpaceCheckSQL("oracle", classification.TableName)
				if spaceSQL != "" {
					spaceRes, spaceErr := exec.Execute(ctx, buildSQLPlusCmd(connStr, spaceSQL))
					if spaceErr == nil {
						if tableSize, freeSize, ok := safety.ParseSpaceCheckResult(spaceRes.Stdout); ok {
							if freeSize < tableSize*1.2 {
								return fmt.Sprintf(
									"⚠️ 백업 공간 부족 (테이블: %.0fMB, 가용: %.0fMB).\n"+
										"백업 없이 진행하려면 skip_backup=true 로 재요청하세요.", tableSize, freeSize), nil
							}
						}
					}
				}
				// 2. 백업 실행
				bakRes, bakErr := exec.Execute(ctx, buildSQLPlusCmd(connStr, classification.BackupSQL))
				if bakErr != nil || bakRes.ExitCode != 0 {
					errOut := bakRes.Stderr
					if bakErr != nil {
						errOut = bakErr.Error()
					}
					return fmt.Sprintf("백업 실패 → 작업 중단:\n%s", errOut), nil
				}
			}

			cmd := buildSQLPlusCmd(connStr, sql)
			res, err := exec.Execute(ctx, cmd)
			if err != nil {
				return fmt.Sprintf("실행 오류: %s", err), nil
			}
			result := formatOutput(res.Stdout, res.Stderr, res.ExitCode)
			if classification.RequiresBackup && !skipBackup && classification.BackupTable != "" {
				result = fmt.Sprintf("[백업: %s]\n%s", classification.BackupTable, result)
			}
			return result, nil
		},
	)
}

func (c *OracleConnector) makeTablespaceTool(prefix, connStr string) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".tablespace",
		fmt.Sprintf("Oracle(%s) 테이블스페이스 사용량 현황 조회.", prefix),
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"target": targetParam()},
		},
		tools.RiskNone, true, &c.status,
		func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
			cmd := buildSQLPlusCmd(connStr, tablespaceQuery)
			res, err := exec.Execute(ctx, cmd)
			if err != nil {
				return fmt.Sprintf("실행 오류: %s", err), nil
			}
			return formatOutput(res.Stdout, res.Stderr, res.ExitCode), nil
		},
	)
}

func (c *OracleConnector) makeSessionsTool(prefix, connStr string) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".sessions",
		fmt.Sprintf("Oracle(%s) 활성 세션 목록 조회 (v$session).", prefix),
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"target": targetParam()},
		},
		tools.RiskNone, true, &c.status,
		func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
			cmd := buildSQLPlusCmd(connStr, sessionsQuery)
			res, err := exec.Execute(ctx, cmd)
			if err != nil {
				return fmt.Sprintf("실행 오류: %s", err), nil
			}
			return formatOutput(res.Stdout, res.Stderr, res.ExitCode), nil
		},
	)
}

func (c *OracleConnector) makeLocksTool(prefix, connStr string) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".locks",
		fmt.Sprintf("Oracle(%s) 락 정보 조회 (v$lock + v$session).", prefix),
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"target": targetParam()},
		},
		tools.RiskNone, true, &c.status,
		func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
			cmd := buildSQLPlusCmd(connStr, locksQuery)
			res, err := exec.Execute(ctx, cmd)
			if err != nil {
				return fmt.Sprintf("실행 오류: %s", err), nil
			}
			return formatOutput(res.Stdout, res.Stderr, res.ExitCode), nil
		},
	)
}

func (c *OracleConnector) makeAlertLogTool(prefix, oracleHome, sid string) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".alert_log",
		fmt.Sprintf("Oracle(%s) alert 로그 마지막 N줄 조회.", prefix),
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"lines":  map[string]interface{}{"type": "integer", "description": "출력 줄 수 (기본 50)"},
				"target": targetParam(),
			},
		},
		tools.RiskNone, true, &c.status,
		func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
			lines := 50
			if v, ok := args["lines"].(float64); ok {
				lines = int(v)
			}
			home := oracleHome
			if home == "" {
				home = "/u01/app/oracle/product"
			}
			cmd := alertLogCmd(home, sid, lines)
			res, err := exec.Execute(ctx, cmd)
			if err != nil {
				return fmt.Sprintf("실행 오류: %s", err), nil
			}
			return formatOutput(res.Stdout, res.Stderr, res.ExitCode), nil
		},
	)
}

func (c *OracleConnector) makeTopSQLTool(prefix, connStr string) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".top_sql",
		fmt.Sprintf("Oracle(%s) 부하 SQL TOP 20 조회 (v$sql elapsed_time 기준).", prefix),
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"target": targetParam()},
		},
		tools.RiskNone, true, &c.status,
		func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
			cmd := buildSQLPlusCmd(connStr, topSQLQuery)
			res, err := exec.Execute(ctx, cmd)
			if err != nil {
				return fmt.Sprintf("실행 오류: %s", err), nil
			}
			return formatOutput(res.Stdout, res.Stderr, res.ExitCode), nil
		},
	)
}

// ProbeOSAuth는 Oracle OS 인증(/ as sysdba)을 시도한다.
// 성공 시 OSAuth=true인 Credentials를 반환한다.
func (c *OracleConnector) ProbeOSAuth(ctx context.Context, info conn.ServiceInfo, exec executor.Executor) (conn.Credentials, error) {
	sid := info.Name
	if sid == "" {
		sid = "ORCL"
	}
	cmd := buildOSAuthProbeCmd(sid)
	res, err := exec.Execute(ctx, cmd)
	if err != nil {
		return conn.Credentials{}, fmt.Errorf("os auth probe: %w", err)
	}
	if !strings.Contains(res.Stdout, "OS_AUTH_OK") {
		return conn.Credentials{}, fmt.Errorf("OS 인증 실패 (SID=%s): sqlplus 접속 불가", sid)
	}
	return conn.Credentials{Username: "/", Role: "sysdba", OSAuth: true}, nil
}

// targetParam은 도구 파라미터에 target 필드를 추가하는 헬퍼이다.
func targetParam() map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": "대상 서버 이름. 생략 시 커넥터 등록 서버 사용.",
	}
}

// formatOutput은 명령 실행 결과를 포맷한다.
// sqlplus 배너·프롬프트를 제거하고 실제 쿼리 결과만 반환한다.
func formatOutput(stdout, stderr string, exitCode int) string {
	out := pipeline.CleanSQLPlusOutput(stdout)
	if exitCode != 0 && stderr != "" {
		return fmt.Sprintf("[ExitCode: %d]\n%s\n[Stderr]\n%s", exitCode, out, strings.TrimSpace(stderr))
	}
	if out == "" {
		return "(결과 없음)"
	}
	return out
}
