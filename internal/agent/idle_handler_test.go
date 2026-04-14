package agent

import (
	"context"
	"testing"
	"time"

	"github.com/yourorg/infractl/internal/executor"
	"github.com/yourorg/infractl/internal/store"
)

type fakeServerStore struct {
	servers []store.Server
}

func (f fakeServerStore) List(ctx context.Context) ([]store.Server, error) {
	return append([]store.Server(nil), f.servers...), nil
}

func (f fakeServerStore) Get(ctx context.Context, name string) (store.Server, error) {
	for _, srv := range f.servers {
		if srv.Name == name {
			return srv, nil
		}
	}
	return store.Server{}, context.Canceled
}

func (f fakeServerStore) Add(ctx context.Context, server store.Server) error    { return nil }
func (f fakeServerStore) Update(ctx context.Context, server store.Server) error { return nil }
func (f fakeServerStore) Remove(ctx context.Context, name string) error         { return nil }
func (f fakeServerStore) Close() error                                          { return nil }

func TestResolveStoredPasswordByTargetName(t *testing.T) {
	st := fakeServerStore{
		servers: []store.Server{
			{
				Name:       "sandbox",
				Host:       "192.168.0.130",
				User:       "sandbox",
				AuthType:   store.AuthTypePassword,
				Credential: "secret123",
			},
		},
	}

	req := IdleInputRequest{
		Target:    "sandbox",
		LastLines: []string{"sandbox@192.168.0.130's password:"},
	}

	got, ok := resolveStoredPassword(context.Background(), st, req)
	if !ok {
		t.Fatal("expected stored password match")
	}
	if got != "secret123" {
		t.Fatalf("expected stored password, got %q", got)
	}
}

func TestResolveStoredPasswordByHostPrompt(t *testing.T) {
	st := fakeServerStore{
		servers: []store.Server{
			{
				Name:       "db",
				Host:       "db.example.com",
				User:       "oracle",
				AuthType:   store.AuthTypePassword,
				Credential: "dbpass",
			},
		},
	}

	req := IdleInputRequest{
		Target:    "",
		LastLines: []string{"oracle@db.example.com's password:"},
	}

	got, ok := resolveStoredPassword(context.Background(), st, req)
	if !ok {
		t.Fatal("expected stored password match")
	}
	if got != "dbpass" {
		t.Fatalf("expected stored password, got %q", got)
	}
}

type fakePersistentStreamExecutor struct {
	sessions   []executor.SessionInfo
	listCalled bool
}

func (f *fakePersistentStreamExecutor) Execute(context.Context, string) (executor.ExecResult, error) {
	return executor.ExecResult{}, nil
}

func (f *fakePersistentStreamExecutor) ExecuteStream(context.Context, string, func(string)) (executor.ExecResult, error) {
	return executor.ExecResult{}, nil
}

func (f *fakePersistentStreamExecutor) InjectStdin(string) error { return nil }
func (f *fakePersistentStreamExecutor) Target() string           { return "db" }

func (f *fakePersistentStreamExecutor) SessionExecute(context.Context, string, string, time.Duration, func([]string) (string, bool)) (executor.ShellRunResult, error) {
	return executor.ShellRunResult{}, nil
}

func (f *fakePersistentStreamExecutor) SessionElevate(context.Context, string, string, time.Duration, func([]string) (string, bool)) (executor.ShellRunResult, error) {
	return executor.ShellRunResult{}, nil
}

func (f *fakePersistentStreamExecutor) SessionClose(context.Context, string) error {
	return nil
}

func (f *fakePersistentStreamExecutor) SessionList(context.Context) ([]executor.SessionInfo, error) {
	f.listCalled = true
	return f.sessions, nil
}

func TestIdleDetectExecutorPreservesPersistentSessions(t *testing.T) {
	original := &fakePersistentStreamExecutor{
		sessions: []executor.SessionInfo{
			{SessionID: "root", CurrentUser: "root", Alive: true},
		},
	}

	wrapped := wrapWithIdleDetect(original, nil, "shell_exec", "db")
	pse, ok := wrapped.(executor.PersistentSessionExecutor)
	if !ok {
		t.Fatal("expected idle wrapper to preserve PersistentSessionExecutor")
	}

	sessions, err := pse.SessionList(context.Background())
	if err != nil {
		t.Fatalf("SessionList() error = %v", err)
	}
	if !original.listCalled {
		t.Fatal("expected SessionList to be forwarded to original executor")
	}
	if len(sessions) != 1 || sessions[0].SessionID != "root" {
		t.Fatalf("unexpected sessions: %#v", sessions)
	}
}
