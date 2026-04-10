// Package connector
// File: learned_command.go
// Description: common learned command schema for synthesized connectors
// Responsibility: provide a stable type shared by activation resolver flows

package connector

// LearnedCommand describes a reconstructed command spec.
type LearnedCommand struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Command       string                 `json:"command"`
	ReadOnly      bool                   `json:"read_only"`
	BackupCommand string                 `json:"backup_command,omitempty"`
	Parameters    map[string]interface{} `json:"parameters,omitempty"`
	Required      []string               `json:"required,omitempty"`
}
