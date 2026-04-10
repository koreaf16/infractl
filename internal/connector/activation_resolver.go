// Package connector
// File: activation_resolver.go
// Description: 외부 RAG/web/probe를 이용한 activation profile 해석기
// Responsibility: capability syntheses의 입력인 system profile과 command specs 생성

package connector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/rag"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
	"github.com/yourorg/infractl/internal/web"
)

// ActivationRequest는 profile synthesis 입력이다.
type ActivationRequest struct {
	ServerName  string
	ServiceType string
	ServiceName string
	Host        string
	Port        int
	Target      string
	OS          string
	EnvProfile  string
	Details     map[string]string
}

// ActivationEvidence는 profile synthesis에 사용된 근거다.
type ActivationEvidence struct {
	Source     string  `json:"source"`
	Title      string  `json:"title,omitempty"`
	URL        string  `json:"url,omitempty"`
	Snippet    string  `json:"snippet,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Category   string  `json:"category,omitempty"`
	ObservedAt string  `json:"observed_at,omitempty"`
}

// ResolvedActivation은 profile, command spec, 질문을 함께 반환한다.
type ResolvedActivation struct {
	Profile     store.SystemProfile
	Commands    map[string]LearnedCommand
	Evidence    []ActivationEvidence
	Questions   []tools.InputQuestion
	Notes       []string
	NeedsInput  bool
	Summary     string
	ServiceKind string
}

// ActivationResolver는 web/rag/probe를 묶어서 profile을 만든다.
type ActivationResolver struct {
	ServerStore  store.ServerStore
	LearnedStore store.LearnedSystemStore
	RAGManager   *rag.Manager
	Fetcher      *web.Fetcher
}

// Resolve는 system profile과 command specs를 생성한다.
func (r *ActivationResolver) Resolve(ctx context.Context, exec executor.Executor, req ActivationRequest) (ResolvedActivation, error) {
	result := ResolvedActivation{}

	server := req.ServerName
	host := req.Host
	port := req.Port
	serviceType := strings.ToLower(strings.TrimSpace(req.ServiceType))
	serviceName := strings.TrimSpace(req.ServiceName)

	if server == "" && exec != nil {
		server = exec.Target()
	}

	if req.Details == nil {
		req.Details = make(map[string]string)
	}

	if host == "" {
		host = req.Details["host"]
	}
	if port == 0 {
		if v := req.Details["port"]; v != "" {
			if p, err := strconv.Atoi(v); err == nil {
				port = p
			}
		}
	}

	if serviceName == "" {
		serviceName = defaultServiceName(serviceType, server)
		result.NeedsInput = true
		result.Questions = append(result.Questions, tools.InputQuestion{
			ID:       "service_name",
			Header:   "Service name",
			Question: fmt.Sprintf("%s 대상의 서비스 이름(service_name)이나 PDB/service alias를 알려주세요. 없으면 기본값으로 진행할 수 있습니다.", strings.Title(serviceType)),
			Default:  serviceName,
		})
	}

	knownServer, _ := r.lookupServer(ctx, server)
	if host == "" && knownServer != nil {
		host = knownServer.Host
	}
	if port == 0 && knownServer != nil {
		port = knownServer.Port
	}
	if req.OS == "" && knownServer != nil {
		req.OS = knownServer.OS
	}
	if req.EnvProfile == "" && knownServer != nil {
		req.EnvProfile = knownServer.EnvProfile
	}

	evidence := r.collectEvidence(ctx, req, exec)
	evidence = append(evidence, r.collectLearnedEvidence(ctx, req)...)

	caps := r.probeCapabilities(ctx, exec, serviceType, req, evidence)
	commands, serviceKind := synthesizeCommands(req, caps, evidence)

	fingerprint := map[string]any{
		"server_name":  server,
		"service_type": serviceType,
		"service_name": serviceName,
		"host":         host,
		"port":         port,
		"os":           req.OS,
		"env_profile":  req.EnvProfile,
		"capabilities": caps,
	}
	fingerprintJSON, _ := json.Marshal(fingerprint)

	profileJSON, _ := json.Marshal(map[string]any{
		"service_kind": serviceKind,
		"service_type": serviceType,
		"service_name": serviceName,
		"host":         host,
		"port":         port,
		"os":           req.OS,
		"env_profile":  req.EnvProfile,
		"capabilities": caps,
		"notes":        result.Notes,
		"evidence":     evidence,
	})
	commandsJSON, _ := json.Marshal(commands)
	evidenceJSON, _ := json.Marshal(evidence)
	questionsJSON, _ := json.Marshal(result.Questions)

	ref := profileRef(server, serviceType, serviceName, host, port, caps, evidence)
	result.Profile = store.SystemProfile{
		ProfileRef:      ref,
		ServerName:      server,
		ServiceType:     serviceType,
		ServiceName:     serviceName,
		Host:            host,
		Port:            port,
		FingerprintJSON: string(fingerprintJSON),
		ProfileJSON:     string(profileJSON),
		CommandsJSON:    string(commandsJSON),
		EvidenceJSON:    string(evidenceJSON),
		QuestionsJSON:   string(questionsJSON),
		Status:          profileStatus(result.NeedsInput),
	}
	result.Commands = commands
	result.Evidence = evidence
	result.ServiceKind = serviceKind
	result.Summary = buildSummary(serviceType, serviceName, serviceKind, caps, result.NeedsInput)
	return result, nil
}

func (r *ActivationResolver) lookupServer(ctx context.Context, name string) (*store.Server, error) {
	if r.ServerStore == nil || strings.TrimSpace(name) == "" {
		return nil, nil
	}
	srv, err := r.ServerStore.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return &srv, nil
}

func (r *ActivationResolver) collectLearnedEvidence(ctx context.Context, req ActivationRequest) []ActivationEvidence {
	if r.LearnedStore == nil || req.ServerName == "" || req.ServiceType == "" {
		return nil
	}
	sys, err := r.LearnedStore.GetLearnedSystem(ctx, req.ServiceType, req.ServerName)
	if err != nil {
		return nil
	}
	return []ActivationEvidence{
		{
			Source:     "learned_systems",
			Title:      fmt.Sprintf("%s @ %s", sys.ServiceType, sys.ServerName),
			Snippet:    summarizeText(sys.Commands, 240),
			Confidence: 0.85,
			Category:   "learned",
			ObservedAt: sys.CreatedAt.Format(time.RFC3339),
		},
	}
}

func (r *ActivationResolver) collectEvidence(ctx context.Context, req ActivationRequest, exec executor.Executor) []ActivationEvidence {
	var evidence []ActivationEvidence

	queryParts := []string{req.ServiceType, req.ServiceName, req.OS, req.EnvProfile}
	query := strings.TrimSpace(strings.Join(filterNonEmpty(queryParts), " "))
	if query == "" {
		query = req.ServiceType
	}
	if query != "" && r.RAGManager != nil {
		if results, err := r.RAGManager.Search(ctx, query+" command line documentation trace tkprof", 5); err == nil {
			for _, res := range results {
				evidence = append(evidence, ActivationEvidence{
					Source:     "rag:" + res.Source,
					Title:      res.Title,
					Snippet:    summarizeText(res.Content, 320),
					Confidence: scoreFromRAG(res),
					Category:   "rag",
				})
			}
		}
	}

	if query != "" {
		webResults, _ := web.SearchDDG(ctx, query+" official documentation command line trace tkprof", 5)
		for _, res := range webResults {
			evidence = append(evidence, ActivationEvidence{
				Source:     "web",
				Title:      res.Title,
				URL:        res.URL,
				Snippet:    summarizeText(res.Snippet, 320),
				Confidence: 0.45,
				Category:   "web",
			})
		}
	}

	// Fetch the top web result if it looks promising.
	if r.Fetcher != nil {
		for _, ev := range evidence {
			if ev.Source != "web" || ev.URL == "" {
				continue
			}
			md, err := r.Fetcher.Fetch(ctx, ev.URL)
			if err != nil {
				continue
			}
			ev.Snippet = summarizeText(md, 400)
			ev.Confidence = 0.55
			evidence = append(evidence, ActivationEvidence{
				Source:     "web_fetch",
				Title:      ev.Title,
				URL:        ev.URL,
				Snippet:    summarizeText(md, 600),
				Confidence: 0.6,
				Category:   "web",
				ObservedAt: time.Now().UTC().Format(time.RFC3339),
			})
			break
		}
	}

	// Probe-derived evidence.
	if exec != nil {
		evidence = append(evidence, r.probeEvidence(ctx, exec, req)...)
	}

	return evidence
}

func (r *ActivationResolver) probeEvidence(ctx context.Context, exec executor.Executor, req ActivationRequest) []ActivationEvidence {
	var evidence []ActivationEvidence
	probes := probePlanFor(req.ServiceType)
	for _, p := range probes {
		res, err := exec.Execute(ctx, p.command)
		if err != nil {
			continue
		}
		if p.matcher != nil && !p.matcher(res.Stdout, res.Stderr, res.ExitCode) {
			continue
		}
		evidence = append(evidence, ActivationEvidence{
			Source:     "probe",
			Title:      p.title,
			Snippet:    summarizeText(formatOutput(res.Stdout, res.Stderr, res.ExitCode), 220),
			Confidence: p.confidence,
			Category:   "probe",
			ObservedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return evidence
}

type probeSpec struct {
	title      string
	command    string
	confidence float64
	matcher    func(stdout, stderr string, exitCode int) bool
}

func probePlanFor(serviceType string) []probeSpec {
	switch strings.ToLower(serviceType) {
	case "oracle":
		return []probeSpec{
			{title: "sqlplus available", command: "which sqlplus", confidence: 0.8, matcher: nonEmptyStdout},
			{title: "tkprof available", command: "which tkprof", confidence: 0.7, matcher: nonEmptyStdout},
			{title: "oracle home", command: "echo ${ORACLE_HOME:-}", confidence: 0.5, matcher: nonEmptyStdout},
			{title: "oracle sid", command: "echo ${ORACLE_SID:-}", confidence: 0.4, matcher: nonEmptyStdout},
		}
	case "mysql":
		return []probeSpec{
			{title: "mysql client available", command: "which mysql", confidence: 0.8, matcher: nonEmptyStdout},
			{title: "mysql version", command: "mysql --version", confidence: 0.6, matcher: nonEmptyStdout},
		}
	case "postgresql", "postgres":
		return []probeSpec{
			{title: "psql available", command: "which psql", confidence: 0.8, matcher: nonEmptyStdout},
			{title: "psql version", command: "psql --version", confidence: 0.6, matcher: nonEmptyStdout},
		}
	case "tomcat":
		return []probeSpec{
			{title: "catalina script", command: "which catalina.sh", confidence: 0.5, matcher: nonEmptyStdout},
			{title: "tomcat process", command: "ps -ef | grep -i '[t]omcat\\|[c]atalina' | head -20", confidence: 0.4, matcher: nonEmptyStdout},
		}
	case "weblogic":
		return []probeSpec{
			{title: "weblogic process", command: "ps -ef | grep -i '[w]eblogic' | head -20", confidence: 0.4, matcher: nonEmptyStdout},
		}
	default:
		return []probeSpec{
			{title: "process inventory", command: "ps -ef | head -40", confidence: 0.3, matcher: nonEmptyStdout},
			{title: "os release", command: "cat /etc/os-release 2>/dev/null | head -20", confidence: 0.2, matcher: nonEmptyStdout},
		}
	}
}

func nonEmptyStdout(stdout, _ string, _ int) bool {
	return strings.TrimSpace(stdout) != ""
}

func synthesizeCommands(req ActivationRequest, _ map[string]bool, evidence []ActivationEvidence) (map[string]LearnedCommand, string) {
	serviceType := strings.ToLower(strings.TrimSpace(req.ServiceType))
	switch serviceType {
	case "oracle":
		return synthesizeOracleCommands(req, evidence), "oracle"
	case "mysql":
		return synthesizeMySQLCommands(req, evidence), "mysql"
	case "postgresql", "postgres":
		return synthesizePostgreSQLCommands(req, evidence), "postgresql"
	case "tomcat":
		return synthesizeTomcatCommands(req, evidence), "tomcat"
	case "weblogic":
		return synthesizeWebLogicCommands(req, evidence), "weblogic"
	default:
		return synthesizeGenericCommands(req, evidence), "generic"
	}
}

func synthesizeOracleCommands(req ActivationRequest, evidence []ActivationEvidence) map[string]LearnedCommand {
	serviceName := defaultServiceName("oracle", req.ServerName)
	if req.ServiceName != "" {
		serviceName = req.ServiceName
	}

	host := defaultIfEmpty(req.Host, "{{host}}")
	port := req.Port
	if port == 0 {
		port = 1521
	}

	connExpr := fmt.Sprintf("{{username}}/{{password}}@%s:%d/%s", host, port, serviceName)
	baseQuery := fmt.Sprintf(`tmp_sql=$(mktemp /tmp/infractl_sql_XXXXXX.sql)
printf '%%s\n' {{q:sql}} > "$tmp_sql"
sqlplus -S '%s' @"$tmp_sql"
rm -f "$tmp_sql"`, connExpr)

	statusCmd := fmt.Sprintf(`sqlplus -S '%s' <<'SQLEOF'
SET FEEDBACK OFF
SET PAGESIZE 0
SELECT 'OK' FROM dual;
EXIT;
SQLEOF`, connExpr)

	traceCmd := fmt.Sprintf(`tmp_dir=$(mktemp -d /tmp/infractl_trace_XXXXXX)
sql_file="$tmp_dir/work.sql"
out_file="$tmp_dir/sqlplus.out"
trace_file="$tmp_dir/trace.trc"
tkprof_file="$tmp_dir/tkprof.txt"
printf '%%s\n' {{q:sql}} > "$sql_file"
sqlplus -S '%s' <<'SQLEOF' > "$out_file"
SET FEEDBACK OFF
SET PAGESIZE 0
SET LINESIZE 200
ALTER SESSION SET tracefile_identifier='{{q:trace_id}}';
BEGIN DBMS_MONITOR.SESSION_TRACE_ENABLE(waits=>TRUE, binds=>TRUE); END;
/
@${sql_file}
BEGIN DBMS_MONITOR.SESSION_TRACE_DISABLE; END;
/
COLUMN value NEW_VALUE TRACEPATH
SELECT value FROM v$diag_info WHERE name='Default Trace File';
EXIT;
SQLEOF
trace_path=$(awk '/Trace file/{print $NF}' "$out_file" | tail -1)
if [ -z "$trace_path" ]; then trace_path="$trace_file"; fi
tkprof "$trace_path" "$tkprof_file" sys=no sort=prsela,exeela,fchela
echo "ARTIFACT kind=trace_file path=$trace_path name=trace.trc description=Oracle trace output"
echo "ARTIFACT kind=tkprof_file path=$tkprof_file name=tkprof.txt description=TKPROF summary"
cat "$tkprof_file"`, connExpr)

	cmds := map[string]LearnedCommand{
		"status": {
			Name:        "status",
			Description: "Oracle instance health and version",
			Command:     statusCmd,
			ReadOnly:    true,
			Parameters: map[string]interface{}{
				"target": targetParam(),
			},
		},
		"query": {
			Name:        "query",
			Description: "Run Oracle SQL",
			Command:     baseQuery,
			ReadOnly:    false,
			Parameters: map[string]interface{}{
				"sql": map[string]interface{}{
					"type":        "string",
					"description": "SQL to execute inside the Oracle session",
				},
				"target": targetParam(),
			},
		},
		"sessions": {
			Name:        "sessions",
			Description: "List Oracle sessions",
			Command:     fmt.Sprintf("sqlplus -S '%s' <<'SQLEOF'\nSET FEEDBACK OFF\nSET PAGESIZE 100\nSELECT sid, serial#, username, status, machine, program, sql_id, event FROM v$session WHERE type = 'USER' ORDER BY status, username;\nEXIT;\nSQLEOF", connExpr),
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
		"locks": {
			Name:        "locks",
			Description: "List Oracle locks",
			Command:     fmt.Sprintf("sqlplus -S '%s' <<'SQLEOF'\nSET FEEDBACK OFF\nSET PAGESIZE 100\nSELECT s.sid, s.serial#, s.username, l.type, l.lmode, l.request, l.block, s.status, s.machine FROM v$lock l JOIN v$session s ON l.sid = s.sid WHERE l.block = 1 OR l.request > 0 ORDER BY l.block DESC;\nEXIT;\nSQLEOF", connExpr),
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
		"alert_log": {
			Name:        "alert_log",
			Description: "Tail Oracle alert log",
			Command: `oracle_home="{{host}}"
if [ -n "{{oracle_home}}" ]; then oracle_home="{{oracle_home}}"; fi
find "$oracle_home/diag/rdbms" -name 'alert_*.log' 2>/dev/null | head -1 | xargs -I{} tail -n {{lines}} {}`,
			ReadOnly: true,
			Parameters: map[string]interface{}{
				"lines":  map[string]interface{}{"type": "integer", "description": "Number of lines to return", "default": 50},
				"target": targetParam(),
			},
		},
		"top_sql": {
			Name:        "top_sql",
			Description: "Show Oracle top SQL",
			Command:     fmt.Sprintf("sqlplus -S '%s' <<'SQLEOF'\nSET FEEDBACK OFF\nSET PAGESIZE 100\nSELECT * FROM (SELECT sql_id, executions, ROUND(elapsed_time/1000000,2) AS elapsed_sec, ROUND(elapsed_time/GREATEST(executions,1)/1000000,4) AS avg_elapsed, SUBSTR(sql_text,1,100) AS sql_text FROM v$sql WHERE executions > 0 ORDER BY elapsed_time DESC) WHERE ROWNUM <= 20;\nEXIT;\nSQLEOF", connExpr),
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
	}

	if hasCapability(evidence, "tkprof") || hasCapability(evidence, "trace") {
		cmds["trace_profile"] = LearnedCommand{
			Name:        "trace_profile",
			Description: "Run Oracle workload with trace enabled and return TKPROF artifacts",
			Command:     traceCmd,
			ReadOnly:    true,
			Parameters: map[string]interface{}{
				"sql": map[string]interface{}{
					"type":        "string",
					"description": "Workload SQL or SQL*Plus script to trace",
				},
				"trace_id": map[string]interface{}{
					"type":        "string",
					"description": "Trace file identifier",
				},
				"target": targetParam(),
			},
			Required: []string{"sql"},
		}
	}

	return cmds
}

func synthesizeMySQLCommands(req ActivationRequest, evidence []ActivationEvidence) map[string]LearnedCommand {
	host := defaultIfEmpty(req.Host, "{{host}}")
	port := req.Port
	if port == 0 {
		port = 3306
	}
	connExpr := fmt.Sprintf("MYSQL_PWD={{q:password}} mysql -u {{q:username}} -h %s -P %d --batch --silent", host, port)

	return map[string]LearnedCommand{
		"query": {
			Name:        "query",
			Description: "Run MySQL SQL",
			Command: fmt.Sprintf(`tmp_sql=$(mktemp /tmp/infractl_sql_XXXXXX.sql)
printf '%%s\n' {{q:sql}} > "$tmp_sql"
%s -e "source $tmp_sql"
rm -f "$tmp_sql"`, connExpr),
			ReadOnly: true,
			Parameters: map[string]interface{}{
				"sql":    map[string]interface{}{"type": "string", "description": "SQL to execute"},
				"target": targetParam(),
			},
			Required: []string{"sql"},
		},
		"status": {
			Name:        "status",
			Description: "Show MySQL status",
			Command:     fmt.Sprintf(`%s -e "SHOW GLOBAL STATUS LIKE 'Threads_%%'; SHOW GLOBAL STATUS LIKE 'Uptime';"`, connExpr),
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
		"processlist": {
			Name:        "processlist",
			Description: "Show MySQL processlist",
			Command:     fmt.Sprintf(`%s -e "SHOW FULL PROCESSLIST;"`, connExpr),
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
		"databases": {
			Name:        "databases",
			Description: "List MySQL databases",
			Command:     fmt.Sprintf(`%s -e "SELECT table_schema AS DatabaseName, ROUND(SUM(data_length+index_length)/1024/1024,1) AS MB FROM information_schema.tables GROUP BY table_schema ORDER BY MB DESC;"`, connExpr),
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
	}
}

func synthesizePostgreSQLCommands(req ActivationRequest, evidence []ActivationEvidence) map[string]LearnedCommand {
	serviceName := defaultServiceName("postgresql", req.ServerName)
	if req.ServiceName != "" {
		serviceName = req.ServiceName
	}
	host := defaultIfEmpty(req.Host, "{{host}}")
	port := req.Port
	if port == 0 {
		port = 5432
	}
	connExpr := fmt.Sprintf("PGPASSWORD={{q:password}} psql -U {{q:username}} -h %s -p %d -d {{q:database}} -At -F ' | '", host, port)

	return map[string]LearnedCommand{
		"query": {
			Name:        "query",
			Description: "Run PostgreSQL SQL",
			Command: fmt.Sprintf(`tmp_sql=$(mktemp /tmp/infractl_sql_XXXXXX.sql)
printf '%%s\n' {{q:sql}} > "$tmp_sql"
%s -f "$tmp_sql"
rm -f "$tmp_sql"`, connExpr),
			ReadOnly: true,
			Parameters: map[string]interface{}{
				"sql":      map[string]interface{}{"type": "string", "description": "SQL to execute"},
				"database": map[string]interface{}{"type": "string", "description": "Database name", "default": serviceName},
				"target":   targetParam(),
			},
			Required: []string{"sql"},
		},
		"connections": {
			Name:        "connections",
			Description: "Show PostgreSQL connections",
			Command:     fmt.Sprintf(`%s -d postgres -c "SELECT pid, usename, datname, state, wait_event_type, wait_event, LEFT(query,80) AS query FROM pg_stat_activity WHERE state IS NOT NULL ORDER BY state;"`, connExpr),
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
		"locks": {
			Name:        "locks",
			Description: "Show PostgreSQL locks",
			Command:     fmt.Sprintf(`%s -d postgres -c "SELECT l.pid, l.mode, l.granted, a.usename, a.state, LEFT(a.query,80) FROM pg_locks l JOIN pg_stat_activity a ON l.pid = a.pid WHERE NOT l.granted OR l.pid IN (SELECT blocking_pid FROM pg_stat_activity WHERE cardinality(pg_blocking_pids(pid)) > 0) ORDER BY l.granted;"`, connExpr),
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
		"databases": {
			Name:        "databases",
			Description: "List PostgreSQL databases",
			Command:     fmt.Sprintf(`%s -d postgres -c "SELECT datname, pg_size_pretty(pg_database_size(datname)) AS size FROM pg_database ORDER BY pg_database_size(datname) DESC;"`, connExpr),
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
	}
}

func synthesizeTomcatCommands(req ActivationRequest, evidence []ActivationEvidence) map[string]LearnedCommand {
	return map[string]LearnedCommand{
		"status": {
			Name:        "status",
			Description: "Show Tomcat status",
			Command:     `ps -ef | grep -i '[t]omcat\|[c]atalina' | head -20`,
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
		"logs": {
			Name:        "logs",
			Description: "Tail Tomcat logs",
			Command:     `if [ -n "{{log_path}}" ] && [ -d "{{log_path}}" ]; then find "{{log_path}}" -name '*.log' -o -name 'catalina*.out' 2>/dev/null | head -3 | xargs -I{} tail -n {{lines}} {}; else ps -ef | grep -i '[t]omcat\|[c]atalina' | head -20; fi`,
			ReadOnly:    true,
			Parameters: map[string]interface{}{
				"lines":  map[string]interface{}{"type": "integer", "default": 80},
				"target": targetParam(),
			},
		},
		"restart": {
			Name:        "restart",
			Description: "Restart Tomcat",
			Command:     `systemctl restart {{service_name}} || ({{catalina_home}}/bin/shutdown.sh && {{catalina_home}}/bin/startup.sh)`,
			ReadOnly:    false,
			Parameters: map[string]interface{}{
				"target": targetParam(),
			},
		},
	}
}

func synthesizeWebLogicCommands(req ActivationRequest, evidence []ActivationEvidence) map[string]LearnedCommand {
	return map[string]LearnedCommand{
		"status": {
			Name:        "status",
			Description: "Show WebLogic status",
			Command:     `ps -ef | grep -i '[w]eblogic' | head -20`,
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
		"logs": {
			Name:        "logs",
			Description: "Tail WebLogic logs",
			Command:     `if [ -n "{{log_path}}" ] && [ -d "{{log_path}}" ]; then find "{{log_path}}" -name '*.log' 2>/dev/null | head -3 | xargs -I{} tail -n {{lines}} {}; else ps -ef | grep -i '[w]eblogic' | head -20; fi`,
			ReadOnly:    true,
			Parameters: map[string]interface{}{
				"lines":  map[string]interface{}{"type": "integer", "default": 80},
				"target": targetParam(),
			},
		},
		"restart": {
			Name:        "restart",
			Description: "Restart WebLogic",
			Command:     `systemctl restart {{service_name}}`,
			ReadOnly:    false,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
	}
}

func synthesizeGenericCommands(req ActivationRequest, evidence []ActivationEvidence) map[string]LearnedCommand {
	return map[string]LearnedCommand{
		"status": {
			Name:        "status",
			Description: "Inspect the target system",
			Command:     `uname -a && ps -ef | head -20`,
			ReadOnly:    true,
			Parameters:  map[string]interface{}{"target": targetParam()},
		},
		"logs": {
			Name:        "logs",
			Description: "Inspect likely log files",
			Command:     `if [ -n "{{log_path}}" ] && [ -d "{{log_path}}" ]; then find "{{log_path}}" -maxdepth 1 -type f | head -5 | xargs -r tail -n {{lines}}; else ps -ef | head -20; fi`,
			ReadOnly:    true,
			Parameters: map[string]interface{}{
				"lines":  map[string]interface{}{"type": "integer", "default": 50},
				"target": targetParam(),
			},
		},
	}
}

func buildSummary(serviceType, serviceName, serviceKind string, caps map[string]bool, needsInput bool) string {
	var capNames []string
	for name, ok := range caps {
		if ok {
			capNames = append(capNames, name)
		}
	}
	sort.Strings(capNames)
	summary := fmt.Sprintf("%s/%s synthesized as %s", serviceType, serviceName, serviceKind)
	if len(capNames) > 0 {
		summary += fmt.Sprintf(" with capabilities: %s", strings.Join(capNames, ", "))
	}
	if needsInput {
		summary += " (needs input)"
	}
	return summary
}

func profileRef(server, serviceType, serviceName, host string, port int, caps map[string]bool, evidence []ActivationEvidence) string {
	h := sha256.New()
	_, _ = h.Write([]byte(strings.ToLower(server)))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(strings.ToLower(serviceType)))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(strings.ToLower(serviceName)))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(strings.ToLower(host)))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(strconv.Itoa(port)))

	var keys []string
	for k, ok := range caps {
		if ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = h.Write([]byte("|" + k))
	}
	for _, ev := range evidence {
		_, _ = h.Write([]byte("|" + ev.Source + ":" + ev.Title))
	}

	sum := h.Sum(nil)
	return fmt.Sprintf("%s_%s", sanitizeToolName(serviceType+"_"+serviceName), fmt.Sprintf("%x", sum[:8]))
}

func profileStatus(needsInput bool) string {
	if needsInput {
		return "needs_input"
	}
	return "prepared"
}

func defaultServiceName(serviceType, server string) string {
	base := strings.TrimSpace(serviceType)
	if base == "" {
		base = "service"
	}
	if server == "" {
		return sanitizeToolName(base)
	}
	return sanitizeToolName(base + "_" + server)
}

func defaultIfEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func summarizeText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

func filterNonEmpty(values []string) []string {
	var out []string
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

func scoreFromRAG(res rag.SearchResult) float64 {
	score := float64(res.Score)
	if score <= 0 {
		score = 0.5
	}
	if res.Priority > 0 {
		score += float64(res.Priority) * 0.05
	}
	return score
}

func hasCapability(evidence []ActivationEvidence, keyword string) bool {
	key := strings.ToLower(strings.TrimSpace(keyword))
	for _, ev := range evidence {
		text := strings.ToLower(ev.Title + " " + ev.Snippet + " " + ev.Source + " " + ev.Category)
		if strings.Contains(text, key) {
			return true
		}
	}
	return false
}
