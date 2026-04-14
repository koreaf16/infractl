// Package generic
// File: connector.go
// Description: [TODO: Add description]
// Responsibility: [TODO: Add responsibility]

package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	conn "github.com/yourorg/infractl/internal/connector"
	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/safety"
	"github.com/yourorg/infractl/internal/tools"
)

var invalidToolName = regexp.MustCompile(`[^a-z0-9_]+`)
var artifactLineRe = regexp.MustCompile(`(?i)^ARTIFACT\s+(.+)$`)

// GenericConnector recreates tools from learned command specs or synthesized profiles.
type GenericConnector struct {
	info        conn.ServiceInfo
	creds       conn.Credentials
	learnedCmds []LearnedCommand
	status      conn.ConnectorStatus
	names       []string
}

// LearnedCommand describes a reconstructed generic command.
type LearnedCommand struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Command       string                 `json:"command"`
	ReadOnly      bool                   `json:"read_only"`
	BackupCommand string                 `json:"backup_command,omitempty"`
	Parameters    map[string]interface{} `json:"parameters,omitempty"`
	Required      []string               `json:"required,omitempty"`
}

// New creates a generic connector.
func New() *GenericConnector {
	return &GenericConnector{status: conn.StatusDisconnected}
}

func (c *GenericConnector) ServiceType() string          { return "generic" }
func (c *GenericConnector) Status() conn.ConnectorStatus { return c.status }
func (c *GenericConnector) ToolNames() []string          { return c.names }

// GenerateTools creates connector tools from learned commands.
func (c *GenericConnector) GenerateTools(info conn.ServiceInfo, creds conn.Credentials) []tools.Tool {
	c.info = info
	c.creds = creds
	c.status = conn.StatusConnected

	if len(c.learnedCmds) == 0 {
		c.learnedCmds = loadLearnedCommands(info.Details["commands_json"])
	}

	serviceName := sanitizeToolName(info.Name)
	if serviceName == "" {
		serviceName = "svc"
	}
	prefix := "generic_" + serviceName

	var toolList []tools.Tool
	for _, cmd := range c.learnedCmds {
		if strings.TrimSpace(cmd.Command) == "" {
			continue
		}
		cmd := cmd
		riskLevel := safety.EnforceRisk(cmd.Command, tools.RiskNone).MinLevel
		params := buildParameters(cmd)

		t := conn.NewGeneratedTool(
			prefix+"."+sanitizeToolName(cmd.Name),
			defaultString(cmd.Description, fmt.Sprintf("Run learned command %s", cmd.Name)),
			params,
			riskLevel,
			cmd.ReadOnly,
			&c.status,
			func(ctx context.Context, args map[string]interface{}, exec executor.Executor) (tools.ToolResult, error) {
				merged := buildTemplateValues(c.info, c.creds, args)
				skipBackup := argBool(args, "skip_backup", false)

				if strings.TrimSpace(cmd.BackupCommand) != "" && !cmd.ReadOnly && !skipBackup {
					backupCmd := renderCommandTemplate(cmd.BackupCommand, merged)
					backupRes, backupErr := exec.Execute(ctx, backupCmd)
					if backupErr != nil || backupRes.ExitCode != 0 {
						errOut := ""
						if backupErr != nil {
							errOut = backupErr.Error()
						} else {
							errOut = formatOutput(backupRes.Stdout, backupRes.Stderr, backupRes.ExitCode)
						}
						return tools.ToolResult{
							Status:  tools.ToolResultError,
							Summary: "Backup command failed",
							Body:    errOut,
						}, nil
					}
				} else if cmd.BackupCommand == "" && tools.RiskOrd(riskLevel) >= tools.RiskOrd(tools.RiskHigh) && !skipBackup {
					return tools.ToolResult{
						Status:  tools.ToolResultError,
						Summary: "Backup command missing for high-risk command",
						Body: fmt.Sprintf(
							"No backup_command was generated for high-risk command %s. Re-run with skip_backup=true or regenerate the profile.",
							cmd.Name,
						),
					}, nil
				}

				rendered := renderCommandTemplate(cmd.Command, merged)
				res, err := exec.Execute(ctx, rendered)
				if err != nil {
					return tools.ToolResult{
						Status:  tools.ToolResultError,
						Summary: fmt.Sprintf("Execution failed: %s", err),
					}, nil
				}

				body := formatOutput(res.Stdout, res.Stderr, res.ExitCode)
				artifacts, body := parseArtifacts(body)
				status := tools.ToolResultOK
				summary := defaultString(cmd.Description, fmt.Sprintf("Run learned command %s", cmd.Name))
				if res.ExitCode != 0 {
					status = tools.ToolResultError
					if body == "" {
						body = formatOutput(res.Stdout, res.Stderr, res.ExitCode)
					}
				}

				return tools.ToolResult{
					Status:    status,
					Summary:   summary,
					Body:      body,
					Artifacts: artifacts,
					Metadata: map[string]any{
						"command":   rendered,
						"exit_code": res.ExitCode,
						"duration":  res.Duration.String(),
					},
				}, nil
			},
		)
		toolList = append(toolList, t)
	}

	c.names = make([]string, len(toolList))
	for i, t := range toolList {
		c.names[i] = t.Name()
	}
	return toolList
}

// AddLearnedCommand appends a learned command before activation.
func (c *GenericConnector) AddLearnedCommand(cmd LearnedCommand) {
	c.learnedCmds = append(c.learnedCmds, cmd)
}

func loadLearnedCommands(raw string) []LearnedCommand {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}

	var commands []LearnedCommand
	for name, value := range payload {
		switch v := value.(type) {
		case string:
			commands = append(commands, LearnedCommand{
				Name:        name,
				Description: fmt.Sprintf("Run learned command %s", name),
				Command:     v,
				ReadOnly:    inferReadOnly(v),
			})
		case map[string]interface{}:
			command, _ := v["command"].(string)
			description, _ := v["description"].(string)
			readOnly, ok := v["read_only"].(bool)
			if !ok {
				readOnly = inferReadOnly(command)
			}
			backupCmd, _ := v["backup_command"].(string)
			toolName := name
			if explicit, ok := v["name"].(string); ok && explicit != "" {
				toolName = explicit
			}

			params := map[string]interface{}{}
			if rawParams, ok := v["parameters"].(map[string]interface{}); ok {
				params = rawParams
			}

			var required []string
			if rawReq, ok := v["required"].([]interface{}); ok {
				for _, item := range rawReq {
					if s, ok := item.(string); ok && s != "" {
						required = append(required, s)
					}
				}
			}

			commands = append(commands, LearnedCommand{
				Name:          toolName,
				Description:   description,
				Command:       command,
				ReadOnly:      readOnly,
				BackupCommand: backupCmd,
				Parameters:    params,
				Required:      required,
			})
		}
	}
	return commands
}

func buildParameters(cmd LearnedCommand) map[string]interface{} {
	params := map[string]interface{}{
		"target": targetParam(),
	}

	for k, v := range cmd.Parameters {
		params[k] = v
	}
	if !cmd.ReadOnly {
		params["skip_backup"] = map[string]interface{}{
			"type":        "boolean",
			"description": "true?�면 ?�전 백업??건너?�니?? 공간 부족이??백업 불필???�인 ?�에�??�용?�세??",
		}
	}

	required := append([]string{}, cmd.Required...)
	if len(required) > 0 {
		return map[string]interface{}{
			"type":       "object",
			"properties": params,
			"required":   required,
		}
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": params,
	}
}

func buildTemplateValues(info conn.ServiceInfo, creds conn.Credentials, args map[string]interface{}) map[string]string {
	values := map[string]string{
		"server_name":  info.ServerName,
		"service_type": info.ServiceType,
		"service_name": info.Name,
		"host":         info.Details["host"],
		"port":         fmt.Sprintf("%d", info.Port),
		"username":     creds.Username,
		"password":     creds.Password,
		"role":         creds.Role,
	}
	for k, v := range info.Details {
		values[k] = v
	}
	for k, v := range args {
		if k == "target" || k == "skip_backup" {
			continue
		}
		switch tv := v.(type) {
		case string:
			values[k] = tv
		case fmt.Stringer:
			values[k] = tv.String()
		default:
			values[k] = fmt.Sprintf("%v", v)
		}
	}
	return values
}

func renderCommandTemplate(template string, values map[string]string) string {
	return templatePlaceholderRe.ReplaceAllStringFunc(template, func(match string) string {
		inner := strings.TrimSpace(match[2 : len(match)-2])
		mode, key := splitTemplateSpec(inner)
		val := values[key]
		switch mode {
		case "", "q", "quote", "shell":
			return shellQuote(val)
		case "raw":
			return val
		default:
			return shellQuote(val)
		}
	})
}

var templatePlaceholderRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

func splitTemplateSpec(spec string) (mode, key string) {
	parts := strings.SplitN(strings.TrimSpace(spec), ":", 2)
	if len(parts) == 1 {
		return "", strings.TrimSpace(parts[0])
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func parseArtifacts(body string) ([]tools.ArtifactRef, string) {
	lines := strings.Split(body, "\n")
	var cleaned []string
	var artifacts []tools.ArtifactRef

	for _, line := range lines {
		matches := artifactLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) == 0 {
			cleaned = append(cleaned, line)
			continue
		}

		artifact := tools.ArtifactRef{Kind: "artifact"}
		fields := strings.Fields(matches[1])
		for _, field := range fields {
			kv := strings.SplitN(field, "=", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(kv[0]))
			val := strings.Trim(strings.TrimSpace(kv[1]), `"'`)
			switch key {
			case "kind":
				artifact.Kind = val
			case "name":
				artifact.Name = val
			case "path":
				artifact.Path = val
			case "content_type":
				artifact.ContentType = val
			case "description":
				artifact.Description = val
			}
		}
		artifacts = append(artifacts, artifact)
	}

	return artifacts, strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func inferReadOnly(command string) bool {
	lower := strings.ToLower(command)
	mutating := []string{" rm ", " mv ", " cp ", " sed -i", " systemctl restart", " systemctl stop", " kill ", "truncate", " drop ", " delete ", " update ", " insert ", " create ", " alter "}
	padded := " " + lower + " "
	for _, marker := range mutating {
		if strings.Contains(padded, marker) {
			return false
		}
	}
	return true
}

func sanitizeToolName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = invalidToolName.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		return "cmd"
	}
	return name
}

func defaultString(v string, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func targetParam() map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": "Target server name. When omitted, the connector's registered server is used.",
	}
}

func formatOutput(stdout, stderr string, exitCode int) string {
	out := strings.TrimSpace(stdout)
	if exitCode != 0 && stderr != "" {
		return fmt.Sprintf("[ExitCode: %d]\n%s\n[Stderr]\n%s", exitCode, out, strings.TrimSpace(stderr))
	}
	if out == "" {
		return "(no output)"
	}
	return out
}

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

