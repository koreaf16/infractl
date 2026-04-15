// Package tools
// File: file_transfer.go
// Description: SFTP 파일 업로드/다운로드 에이전트 도구 — scp 대체
// Responsibility: 인증된 SSH 연결을 재사용한 SFTP로 파일을 전송한다 (패스워드 프롬프트 없음)

package tools

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/yourorg/infractl/internal/executor"
)

// FileTransferTool transfers files between the local controller and a remote server via SFTP.
// Uses the existing authenticated SSH connection — no password prompt.
type FileTransferTool struct {
	OutputCb func(string)
}

func (t *FileTransferTool) Name() string { return "file_transfer" }

func (t *FileTransferTool) Description() string {
	return "Upload or download a file between this local controller and a remote server via SFTP.\n" +
		"ALWAYS use this tool instead of shell_exec + scp for file transfers.\n" +
		"Reuses the existing authenticated SSH connection — no password prompt, no PTY required.\n" +
		"action=upload: copies local_path on this PC to remote_path on the target server.\n" +
		"action=download: copies remote_path on the target server to local_path on this PC.\n" +
		"Always include 'description' field with a brief Korean explanation."
}

func (t *FileTransferTool) IsReadOnly() bool { return false }
func (t *FileTransferTool) IsEnabled() bool  { return true }

func (t *FileTransferTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"upload", "download"},
				"description": "Transfer direction: 'upload' sends local_path to remote_path; 'download' fetches remote_path to local_path.",
			},
			"local_path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path on the local controller (this PC).",
			},
			"remote_path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path on the remote server.",
			},
			"target": map[string]interface{}{
				"type":        "string",
				"description": "Target server name (same as shell_exec target). Required.",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Brief Korean description of what this transfer does. Shown to the user in the TUI.",
			},
		},
		"required": []string{"action", "local_path", "remote_path", "target"},
	}
}

func (t *FileTransferTool) Execute(ctx context.Context, args map[string]interface{}, exec executor.Executor) (string, error) {
	action, err := argString(args, "action", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	localPath, err := argString(args, "local_path", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}
	remotePath, err := argString(args, "remote_path", true)
	if err != nil {
		return fmt.Sprintf("Error: %s", err), nil
	}

	ft, ok := exec.(executor.FileTransferExecutor)
	if !ok {
		target := exec.Target()
		if target == "" || target == "localhost" {
			return "Error: file_transfer is only for remote servers. You are currently on 'localhost'. Please specify a remote server name in the 'target' argument.", nil
		}
		return fmt.Sprintf("Error: target server %q does not support file transfer (SFTP).", target), nil
	}

	var lastPct int64
	onProgress := func(transferred, total int64) {
		if t.OutputCb == nil || total == 0 {
			return
		}
		pct := transferred * 100 / total
		if pct-lastPct >= 10 {
			lastPct = pct
			t.OutputCb(fmt.Sprintf("%d%% (%s / %s)", pct, formatBytes(transferred), formatBytes(total)))
		}
	}

	switch action {
	case "upload":
		// Pre-flight: check local file exists and get its size.
		fi, statErr := os.Stat(localPath)
		if statErr != nil {
			return fmt.Sprintf("Error: cannot read local file %q: %s", localPath, statErr), nil
		}
		remoteDir := path.Dir(remotePath)
		if checkErr := checkRemoteWritable(ctx, exec, remoteDir); checkErr != nil {
			return fmt.Sprintf("Pre-flight check failed: %s", checkErr), nil
		}
		if checkErr := checkRemoteDiskSpace(ctx, exec, remoteDir, fi.Size()); checkErr != nil {
			return fmt.Sprintf("Pre-flight check failed: %s", checkErr), nil
		}
		if t.OutputCb != nil {
			t.OutputCb(fmt.Sprintf("Uploading %s → %s:%s", localPath, exec.Target(), remotePath))
		}
		if err := ft.Upload(ctx, localPath, remotePath, onProgress); err != nil {
			return fmt.Sprintf("Upload failed: %s", err), nil
		}
		return fmt.Sprintf("Upload complete: %s → %s:%s", localPath, exec.Target(), remotePath), nil

	case "download":
		if t.OutputCb != nil {
			t.OutputCb(fmt.Sprintf("Downloading %s:%s → %s", exec.Target(), remotePath, localPath))
		}
		if err := ft.Download(ctx, remotePath, localPath, onProgress); err != nil {
			return fmt.Sprintf("Download failed: %s", err), nil
		}
		return fmt.Sprintf("Download complete: %s:%s → %s", exec.Target(), remotePath, localPath), nil

	default:
		return fmt.Sprintf("Error: unknown action %q, must be 'upload' or 'download'", action), nil
	}
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
