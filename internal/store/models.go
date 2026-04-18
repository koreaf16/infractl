package store

import "time"

type AuthType string

const (
	AuthTypeKey      AuthType = "key"
	AuthTypePassword AuthType = "password"
)

// Server is the persisted SSH-backed workspace definition.
// The name is kept for schema/API compatibility; user-facing surfaces call it
// a workspace.
type Server struct {
	ID           int
	Name         string
	Host         string
	Port         int
	User         string
	AuthType     AuthType
	Credential   string
	OS           string
	EnvProfile   string
	WorkspaceDir string
	CreatedAt    time.Time
}
