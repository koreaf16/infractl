package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
)

var osMajorVersionPattern = regexp.MustCompile(`\b([0-9]{1,2})(?:\.[0-9]+)?\b`)

func normalizeWebSearchQueryByOS(
	ctx context.Context,
	args map[string]interface{},
	target string,
	activeServer *store.Server,
	serverStore store.ServerStore,
) {
	queryValue, ok := args["query"]
	if !ok || queryValue == nil {
		return
	}
	query, ok := queryValue.(string)
	if !ok {
		query = fmt.Sprintf("%v", queryValue)
	}
	query = strings.TrimSpace(query)
	if query == "" || !isEnvironmentSensitiveWebSearchQuery(query) {
		return
	}

	osText := resolveTargetOSForWebSearch(ctx, target, activeServer, serverStore)
	hints := buildWebSearchOSHints(osText)
	if len(hints) == 0 {
		return
	}

	args["query"] = mergeQueryWithHints(query, hints)
}

func resolveTargetOSForWebSearch(
	ctx context.Context,
	target string,
	activeServer *store.Server,
	serverStore store.ServerStore,
) string {
	if activeServer != nil && strings.TrimSpace(activeServer.OS) != "" {
		if target == "" || strings.EqualFold(target, activeServer.Name) {
			return strings.TrimSpace(activeServer.OS)
		}
	}

	if serverStore == nil || executor.IsLocalTarget(target) {
		return ""
	}

	srv, err := serverStore.Get(ctx, target)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(srv.OS)
}

func isEnvironmentSensitiveWebSearchQuery(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}

	keywords := []string{
		"install", "installation", "setup", "configure", "configuration", "config",
		"guide", "troubleshoot", "troubleshooting", "error", "fix",
		"설치", "설정", "구성", "가이드", "오류", "에러", "트러블슈팅", "문제해결", "문제 해결",
	}
	for _, kw := range keywords {
		if strings.Contains(q, kw) {
			return true
		}
	}
	return false
}

func buildWebSearchOSHints(osText string) []string {
	low := strings.ToLower(strings.TrimSpace(osText))
	if low == "" {
		return nil
	}

	major := extractOSMajorVersion(low)
	if major == "" {
		return nil
	}

	switch {
	case strings.Contains(low, "rocky"):
		return []string{
			fmt.Sprintf("Rocky Linux %s", major),
			fmt.Sprintf("RHEL %s compatible", major),
		}
	case strings.Contains(low, "red hat"), strings.Contains(low, "redhat"), strings.Contains(low, "rhel"):
		return []string{fmt.Sprintf("RHEL %s", major)}
	case strings.Contains(low, "oracle linux"):
		return []string{fmt.Sprintf("Oracle Linux %s", major)}
	case strings.Contains(low, "ubuntu"):
		return []string{fmt.Sprintf("Ubuntu %s", major)}
	case strings.Contains(low, "debian"):
		return []string{fmt.Sprintf("Debian %s", major)}
	case strings.Contains(low, "centos"):
		return []string{fmt.Sprintf("CentOS %s", major)}
	case strings.Contains(low, "amazon linux"):
		return []string{fmt.Sprintf("Amazon Linux %s", major)}
	default:
		return nil
	}
}

func extractOSMajorVersion(osTextLower string) string {
	m := osMajorVersionPattern.FindStringSubmatch(osTextLower)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func mergeQueryWithHints(query string, hints []string) string {
	out := strings.TrimSpace(query)
	if out == "" {
		return query
	}

	low := strings.ToLower(out)
	for _, hint := range hints {
		h := strings.TrimSpace(hint)
		if h == "" {
			continue
		}
		if strings.Contains(low, strings.ToLower(h)) {
			continue
		}
		out += " " + h
		low = strings.ToLower(out)
	}
	return out
}
