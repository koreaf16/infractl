package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
	"github.com/yourorg/infractl/internal/tools"
)

type connectorEntry struct {
	connector Connector
	state     ConnectorState
}

// ConnectorFactory creates a connector for a specific service type.
type ConnectorFactory func() Connector

// Manager manages connector lifecycle and registry integration.
type Manager struct {
	registry    *tools.Registry
	store       store.ConnectorStore
	entries     map[string]*connectorEntry
	factories   map[string]ConnectorFactory
	toolTargets map[string]string
	connectHook func(ctx context.Context, server, serviceType string)
	mu          sync.RWMutex
}

// NewManager creates a connector manager.
func NewManager(registry *tools.Registry, st store.ConnectorStore) *Manager {
	return &Manager{
		registry:    registry,
		store:       st,
		entries:     make(map[string]*connectorEntry),
		factories:   make(map[string]ConnectorFactory),
		toolTargets: make(map[string]string),
	}
}

// SetConnectHook configures the callback fired after activation.
func (m *Manager) SetConnectHook(fn func(ctx context.Context, server, serviceType string)) {
	m.connectHook = fn
}

// RegisterFactory registers a connector factory by service type.
func (m *Manager) RegisterFactory(serviceType string, factory ConnectorFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factories[serviceType] = factory
}

// ProbeAndActivate는 OS 인증을 먼저 시도하고 성공하면 커넥터를 활성화한다.
// 커넥터가 OSAuthProber를 구현하지 않거나 OS 인증이 실패하면 non-nil error를 반환한다.
// 반환 값은 활성화된 도구 수이다.
func (m *Manager) ProbeAndActivate(ctx context.Context, info ServiceInfo, exec executor.Executor, saveMode SaveMode) (int, error) {
	conn, err := m.createConnector(info.ServiceType)
	if err != nil {
		return 0, fmt.Errorf("create connector: %w", err)
	}

	prober, ok := conn.(OSAuthProber)
	if !ok {
		return 0, fmt.Errorf("서비스 %s는 OS 인증을 지원하지 않습니다", info.ServiceType)
	}

	creds, err := prober.ProbeOSAuth(ctx, info, exec)
	if err != nil {
		return 0, err
	}

	if err := m.Activate(ctx, info, creds, saveMode); err != nil {
		return 0, fmt.Errorf("activate after os auth: %w", err)
	}

	m.mu.RLock()
	key := connectorKey(info.ServerName, info.ServiceType, info.Name)
	entry, exists := m.entries[key]
	m.mu.RUnlock()
	if exists {
		return len(entry.state.Tools), nil
	}
	return 0, nil
}

// Activate creates a connector instance and registers its tools.
func (m *Manager) Activate(ctx context.Context, info ServiceInfo, creds Credentials, saveMode SaveMode) error {
	key := connectorKey(info.ServerName, info.ServiceType, info.Name)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.entries[key]; exists {
		if err := m.deactivateLocked(key); err != nil {
			slog.Warn("deactivate existing connector", "key", key, "err", err)
		}
	}

	conn, err := m.createConnector(info.ServiceType)
	if err != nil {
		return fmt.Errorf("create connector: %w", err)
	}

	toolList := conn.GenerateTools(info, creds)
	toolNames := make([]string, len(toolList))
	for i, t := range toolList {
		if regErr := m.registry.Register(t); regErr != nil {
			slog.Warn("register connector tool", "name", t.Name(), "err", regErr)
		}
		toolNames[i] = t.Name()
		m.toolTargets[t.Name()] = info.ServerName
	}

	state := ConnectorState{
		Name:        key,
		ServerName:  info.ServerName,
		Type:        info.ServiceType,
		ServiceName: info.Name,
		Status:      StatusConnected,
		Tools:       toolNames,
	}
	m.entries[key] = &connectorEntry{connector: conn, state: state}

	slog.Info("connector activated", "key", key, "tools", len(toolList))

	if m.connectHook != nil {
		m.connectHook(ctx, info.ServerName, info.ServiceType)
	}

	if saveMode == SavePermanent && m.store != nil {
		if err := m.persistConnector(ctx, info, creds, toolNames, saveMode); err != nil {
			slog.Warn("persist connector", "key", key, "err", err)
		}
	}

	return nil
}

// Deactivate removes an active connector.
func (m *Manager) Deactivate(serverName, serviceType, serviceName string) error {
	key := connectorKey(serverName, serviceType, serviceName)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deactivateLocked(key)
}

func (m *Manager) deactivateLocked(key string) error {
	entry, ok := m.entries[key]
	if !ok {
		return fmt.Errorf("connector not found: %s", key)
	}
	for _, name := range entry.connector.ToolNames() {
		m.registry.Unregister(name)
		delete(m.toolTargets, name)
	}
	delete(m.entries, key)
	slog.Info("connector deactivated", "key", key)
	return nil
}

// LoadSaved restores saved permanent connectors.
func (m *Manager) LoadSaved(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	entries, err := m.store.ListPermanentConnectors(ctx)
	if err != nil {
		return fmt.Errorf("list permanent connectors: %w", err)
	}

	for _, e := range entries {
		creds, err := m.decryptCreds(e.Credentials)
		if err != nil {
			slog.Warn("decrypt connector creds", "server", e.ServerName, "err", err)
			continue
		}

		var details map[string]string
		if e.Config != "" {
			_ = json.Unmarshal([]byte(e.Config), &details)
		}
		if details == nil {
			details = make(map[string]string)
		}

		info := ServiceInfo{
			ServerName:  e.ServerName,
			ServiceType: e.ServiceType,
			Name:        e.ServiceName,
			Details:     details,
		}

		if err := m.Activate(ctx, info, creds, SaveSession); err != nil {
			slog.Warn("reload connector", "server", e.ServerName, "type", e.ServiceType, "err", err)
		}
	}

	return nil
}

// States returns active connector states.
func (m *Manager) States() []ConnectorState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	states := make([]ConnectorState, 0, len(m.entries))
	for _, e := range m.entries {
		states = append(states, e.state)
	}
	return states
}

// DefaultTargetForTool returns the connector server registered for a tool.
func (m *Manager) DefaultTargetForTool(toolName string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.toolTargets[toolName]
}

// DeactivateServer removes active and persisted connectors bound to a server.
func (m *Manager) DeactivateServer(ctx context.Context, serverName string) error {
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return nil
	}

	m.mu.Lock()
	var keys []string
	for key, entry := range m.entries {
		if strings.EqualFold(entry.state.ServerName, serverName) {
			keys = append(keys, key)
		}
	}

	var errs []string
	for _, key := range keys {
		if err := m.deactivateLocked(key); err != nil {
			errs = append(errs, err.Error())
		}
	}
	m.mu.Unlock()

	if m.store != nil {
		entries, err := m.store.ListConnectors(ctx)
		if err != nil {
			errs = append(errs, fmt.Sprintf("list persisted connectors: %v", err))
		} else {
			for _, entry := range entries {
				if !strings.EqualFold(entry.ServerName, serverName) {
					continue
				}
				if err := m.store.RemoveConnector(ctx, entry.ServerName, entry.ServiceType, entry.ServiceName); err != nil {
					errs = append(errs, err.Error())
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("deactivate connectors for %s: %s", serverName, strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) persistConnector(ctx context.Context, info ServiceInfo, creds Credentials, toolNames []string, saveMode SaveMode) error {
	configJSON, _ := json.Marshal(info.Details)

	osAuthStr := ""
	if creds.OSAuth {
		osAuthStr = "true"
	}
	credsJSON, _ := json.Marshal(map[string]string{
		"username": creds.Username,
		"password": creds.Password,
		"role":     creds.Role,
		"os_auth":  osAuthStr,
	})
	encryptedCreds, err := m.store.EncryptConnectorCreds(string(credsJSON))
	if err != nil {
		return fmt.Errorf("encrypt credentials: %w", err)
	}

	toolsJSON, _ := json.Marshal(toolNames)

	return m.store.SaveConnector(ctx, store.ConnectorEntry{
		ServerName:  info.ServerName,
		ServiceType: info.ServiceType,
		ServiceName: info.Name,
		Config:      string(configJSON),
		Credentials: encryptedCreds,
		Tools:       string(toolsJSON),
		SaveMode:    string(saveMode),
	})
}

func (m *Manager) decryptCreds(encrypted string) (Credentials, error) {
	if encrypted == "" {
		return Credentials{}, nil
	}
	plaintext, err := m.store.DecryptConnectorCreds(encrypted)
	if err != nil {
		return Credentials{}, fmt.Errorf("decrypt: %w", err)
	}

	var raw map[string]string
	if err := json.Unmarshal([]byte(plaintext), &raw); err != nil {
		return Credentials{}, fmt.Errorf("unmarshal creds: %w", err)
	}
	return Credentials{
		Username: raw["username"],
		Password: raw["password"],
		Role:     raw["role"],
		OSAuth:   raw["os_auth"] == "true",
	}, nil
}

func (m *Manager) createConnector(serviceType string) (Connector, error) {
	factory, ok := m.factories[serviceType]
	if !ok {
		return nil, fmt.Errorf("unknown service type (factory not registered): %s", serviceType)
	}
	return factory(), nil
}
