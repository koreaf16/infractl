// Package mysql
// File: connector.go
// Description: MySQL DB 커넥터 — mysql CLI 기반 도구 세트 생성
// Responsibility: MySQL 접속 정보로 query/status/processlist/databases 도구 생성

package mysql

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

// MySQLConnector는 MySQL 전용 커넥터이다.
type MySQLConnector struct {
	info   conn.ServiceInfo
	creds  conn.Credentials
	status conn.ConnectorStatus
	names  []string
}

// New는 MySQLConnector를 생성한다.
func New() *MySQLConnector {
	return &MySQLConnector{status: conn.StatusDisconnected}
}

func (c *MySQLConnector) ServiceType() string           { return "mysql" }
func (c *MySQLConnector) Status() conn.ConnectorStatus  { return c.status }
func (c *MySQLConnector) ToolNames() []string           { return c.names }

// GenerateTools는 MySQL 접속 정보로 도구를 생성한다.
func (c *MySQLConnector) GenerateTools(info conn.ServiceInfo, creds conn.Credentials) []tools.Tool {
	c.info = info
	c.creds = creds
	c.status = conn.StatusConnected

	dbName := info.Name
	if dbName == "" {
		dbName = "mysql"
	}
	prefix := "mysql_" + strings.ToLower(dbName)

	host := info.Details["host"]
	if host == "" {
		host = "127.0.0.1"
	}
	port := info.Port
	if port == 0 {
		port = 3306
	}

	toolList := []tools.Tool{
		c.makeQueryTool(prefix, creds.Username, creds.Password, host, port),
		c.makeStatusTool(prefix, creds.Username, creds.Password, host, port),
		c.makeProcesslistTool(prefix, creds.Username, creds.Password, host, port),
		c.makeDatabasesTool(prefix, creds.Username, creds.Password, host, port),
	}

	c.names = make([]string, len(toolList))
	for i, t := range toolList {
		c.names[i] = t.Name()
	}
	return toolList
}

// buildCmd는 MYSQL_PWD 환경변수로 비밀번호를 전달하는 mysql 명령을 생성한다.
// 비밀번호를 커맨드라인 인자에 직접 넣지 않아 ps 노출을 방지한다.
func buildCmd(user, pass, host string, port int, sql string) string {
	return fmt.Sprintf(
		`MYSQL_PWD='%s' mysql -u '%s' -h '%s' -P %d --batch --silent -e "%s"`,
		strings.ReplaceAll(pass, "'", "'\\''"),
		user, host, port, sql,
	)
}

// buildSocketCmd는 유닉스 소켓을 통한 OS 인증 mysql 명령을 생성한다.
func buildSocketCmd(socketPath, sql string) string {
	return fmt.Sprintf(`mysql -u root --socket='%s' --batch --silent -e "%s"`, socketPath, sql)
}

// ProbeOSAuth는 유닉스 소켓으로 MySQL OS 인증을 시도한다.
func (c *MySQLConnector) ProbeOSAuth(ctx context.Context, info conn.ServiceInfo, exec executor.Executor) (conn.Credentials, error) {
	sockets := []string{"/var/run/mysqld/mysqld.sock", "/tmp/mysql.sock"}
	for _, sock := range sockets {
		cmd := buildSocketCmd(sock, "SELECT 'OS_AUTH_OK';")
		res, err := exec.Execute(ctx, cmd)
		if err == nil && strings.Contains(res.Stdout, "OS_AUTH_OK") {
			return conn.Credentials{Username: "root", OSAuth: true}, nil
		}
	}
	return conn.Credentials{}, fmt.Errorf("MySQL unix socket 인증 실패: 소켓 없음 또는 접근 거부")
}

func (c *MySQLConnector) makeQueryTool(prefix, user, pass, host string, port int) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".query",
		fmt.Sprintf("MySQL(%s) SQL 실행. SELECT/DML 모두 가능.", prefix),
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
				spaceSQL := safety.SpaceCheckSQL("mysql", classification.TableName)
				if spaceSQL != "" {
					spaceRes, spaceErr := exec.Execute(ctx, buildCmd(user, pass, host, port, spaceSQL))
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
				bakRes, bakErr := exec.Execute(ctx, buildCmd(user, pass, host, port, classification.BackupSQL))
				if bakErr != nil || bakRes.ExitCode != 0 {
					errOut := bakRes.Stderr
					if bakErr != nil {
						errOut = bakErr.Error()
					}
					return fmt.Sprintf("백업 실패 → 작업 중단:\n%s", errOut), nil
				}
			}

			res, err := exec.Execute(ctx, buildCmd(user, pass, host, port, sql))
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

func (c *MySQLConnector) makeStatusTool(prefix, user, pass, host string, port int) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".status",
		fmt.Sprintf("MySQL(%s) 서버 상태 조회 (SHOW STATUS).", prefix),
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"target": targetParam()},
		},
		tools.RiskNone, true, &c.status,
		func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
			res, err := exec.Execute(ctx, buildCmd(user, pass, host, port, "SHOW GLOBAL STATUS LIKE 'Threads_%'; SHOW GLOBAL STATUS LIKE 'Uptime';"))
			if err != nil {
				return fmt.Sprintf("실행 오류: %s", err), nil
			}
			return formatOutput(res.Stdout, res.Stderr, res.ExitCode), nil
		},
	)
}

func (c *MySQLConnector) makeProcesslistTool(prefix, user, pass, host string, port int) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".processlist",
		fmt.Sprintf("MySQL(%s) 활성 프로세스 목록 조회 (SHOW PROCESSLIST).", prefix),
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"target": targetParam()},
		},
		tools.RiskNone, true, &c.status,
		func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
			res, err := exec.Execute(ctx, buildCmd(user, pass, host, port, "SHOW FULL PROCESSLIST;"))
			if err != nil {
				return fmt.Sprintf("실행 오류: %s", err), nil
			}
			return formatOutput(res.Stdout, res.Stderr, res.ExitCode), nil
		},
	)
}

func (c *MySQLConnector) makeDatabasesTool(prefix, user, pass, host string, port int) tools.Tool {
	return conn.NewConnectorTool(
		prefix+".databases",
		fmt.Sprintf("MySQL(%s) 데이터베이스 목록 및 크기 조회.", prefix),
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"target": targetParam()},
		},
		tools.RiskNone, true, &c.status,
		func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
			sql := "SELECT table_schema AS 'Database', ROUND(SUM(data_length+index_length)/1024/1024,1) AS 'MB' FROM information_schema.tables GROUP BY table_schema ORDER BY MB DESC;"
			res, err := exec.Execute(ctx, buildCmd(user, pass, host, port, sql))
			if err != nil {
				return fmt.Sprintf("실행 오류: %s", err), nil
			}
			return formatOutput(res.Stdout, res.Stderr, res.ExitCode), nil
		},
	)
}

func targetParam() map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": "대상 서버 이름. 생략 시 커넥터 등록 서버 사용.",
	}
}

// formatOutput은 명령 실행 결과를 포맷한다.
// mysql 배너·프롬프트를 제거하고 실제 쿼리 결과만 반환한다.
func formatOutput(stdout, stderr string, exitCode int) string {
	out := pipeline.CleanMySQLOutput(stdout)
	if exitCode != 0 && stderr != "" {
		return fmt.Sprintf("[ExitCode: %d]\n%s\n[Stderr]\n%s", exitCode, out, strings.TrimSpace(stderr))
	}
	if out == "" {
		return "(결과 없음)"
	}
	return out
}
