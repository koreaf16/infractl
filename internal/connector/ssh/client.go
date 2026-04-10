// Package ssh
// File: client.go
// Description: SSH 클라이언트 — 연결 관리, 명령 실행, keep-alive
// Responsibility: SSH 연결 생명주기(연결, 재연결, keep-alive, 종료) 관리

package ssh

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/yourorg/infractl/internal/executor"
)

const (
	defaultKeepAlive = 30 * time.Second
	defaultTimeout   = 30 * time.Second
)

// RunResult는 원격 명령 실행 결과를 담는다.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Client는 단일 서버에 대한 SSH 연결을 관리한다.
// 첫 Execute() 호출 시 Lazy 연결하며, 이후 keep-alive로 유지한다.
type Client struct {
	cfg       *Config
	mu        sync.Mutex
	conn      *ssh.Client
	connected bool
	stopKA    context.CancelFunc // keep-alive 고루틴 취소 함수

	sessionMu sync.Mutex
	stdinPipe io.WriteCloser // RunStream 실행 중에만 유효; InjectStdin이 사용
}

// NewClient는 SSH 클라이언트를 생성한다. 아직 연결하지 않는다.
func NewClient(cfg *Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.KeepAlive == 0 {
		cfg.KeepAlive = defaultKeepAlive
	}
	return &Client{cfg: cfg}
}

// Run은 원격 서버에서 command를 실행하고 결과를 반환한다.
// 출력은 64KB로 제한된다.
func (c *Client) Run(ctx context.Context, command string) (RunResult, error) {
	start := time.Now()

	session, err := c.newSession(ctx)
	if err != nil {
		return RunResult{}, err
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &executor.LimitedWriter{Buf: &stdoutBuf, Limit: executor.MaxOutputBytes}
	session.Stderr = &executor.LimitedWriter{Buf: &stderrBuf, Limit: executor.MaxOutputBytes}

	runErr := session.Run(command)
	duration := time.Since(start)

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			return RunResult{}, fmt.Errorf("ssh run command: %w", runErr)
		}
	}

	return RunResult{
		Stdout:   executor.TruncateOutput(stdoutBuf.String(), executor.MaxOutputBytes),
		Stderr:   executor.TruncateOutput(stderrBuf.String(), executor.MaxOutputBytes),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

// RunStream은 command를 비블로킹으로 실행하며 stdout을 라인 단위로 스트리밍한다.
// onLine: 각 stdout 라인을 수신할 콜백. nil이면 무시.
// onIdle: 출력이 defaultIdleThreshold(10초) 동안 없을 때 호출. nil이면 무시.
// 출력은 64KB로 제한된다.
func (c *Client) RunStream(
	ctx context.Context,
	command string,
	onLine func(string),
	onIdle func(executor.StdinInjector),
) (RunResult, error) {
	start := time.Now()

	session, err := c.newSession(ctx)
	if err != nil {
		return RunResult{}, err
	}
	defer session.Close()

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("ssh stdin pipe: %w", err)
	}
	c.sessionMu.Lock()
	c.stdinPipe = stdinPipe
	c.sessionMu.Unlock()
	defer func() {
		c.sessionMu.Lock()
		c.stdinPipe = nil
		c.sessionMu.Unlock()
		stdinPipe.Close()
	}()

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return RunResult{}, fmt.Errorf("ssh stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	session.Stderr = &executor.LimitedWriter{Buf: &stderrBuf, Limit: executor.MaxOutputBytes}

	if err := session.Start(command); err != nil {
		return RunResult{}, fmt.Errorf("ssh start command: %w", err)
	}

	watcher := executor.NewIdleWatcher(0) // 0 = 기본 10초
	watcher.Start(ctx)
	defer watcher.Stop()

	if onIdle != nil {
		go func() {
			select {
			case <-watcher.Triggered():
				onIdle(c)
			case <-ctx.Done():
			}
		}()
	}

	var stdoutLines []string
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, executor.MaxOutputBytes), executor.MaxOutputBytes)
	for scanner.Scan() {
		line := scanner.Text()
		watcher.Ping()
		stdoutLines = append(stdoutLines, line)
		if onLine != nil {
			onLine(line)
		}
	}

	waitErr := session.Wait()
	duration := time.Since(start)

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			return RunResult{}, fmt.Errorf("ssh wait command: %w", waitErr)
		}
	}

	return RunResult{
		Stdout:   executor.TruncateOutput(strings.Join(stdoutLines, "\n"), executor.MaxOutputBytes),
		Stderr:   executor.TruncateOutput(stderrBuf.String(), executor.MaxOutputBytes),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

// InjectStdin은 현재 RunStream으로 실행 중인 프로세스의 stdin에 line+"\n"을 쓴다.
// executor.StdinInjector 인터페이스를 구현한다.
func (c *Client) InjectStdin(line string) error {
	c.sessionMu.Lock()
	pipe := c.stdinPipe
	c.sessionMu.Unlock()

	if pipe == nil {
		return fmt.Errorf("inject stdin: no active session stdin pipe")
	}
	if _, err := fmt.Fprintln(pipe, line); err != nil {
		return fmt.Errorf("inject stdin: %w", err)
	}
	return nil
}

// Close는 SSH 연결과 keep-alive 고루틴을 종료한다.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stopKA != nil {
		c.stopKA()
		c.stopKA = nil
	}
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.connected = false
		return err
	}
	return nil
}

// newSession은 연결을 보장하고 새 SSH 세션을 반환한다.
// 세션 생성에 실패하면 한 번 재연결을 시도한다.
func (c *Client) newSession(ctx context.Context) (*ssh.Session, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("ssh connect %s: %w", c.cfg.Host, err)
	}

	session, err := c.conn.NewSession()
	if err == nil {
		return session, nil
	}

	// 세션 생성 실패 → 연결이 끊긴 것으로 간주하고 재연결 시도
	c.markDisconnected()
	if err2 := c.ensureConnected(ctx); err2 != nil {
		return nil, fmt.Errorf("ssh reconnect %s: %w", c.cfg.Host, err2)
	}
	session, err = c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh new session: %w", err)
	}
	return session, nil
}

// ensureConnected는 연결이 없으면 SSH 연결을 수립한다.
func (c *Client) ensureConnected(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected && c.conn != nil {
		return nil
	}
	return c.connect(ctx)
}

// connect는 SSH 연결을 수립한다. 호출 전 c.mu를 잠가야 한다.
func (c *Client) connect(ctx context.Context) error {
	authMethods, err := c.buildAuthMethods()
	if err != nil {
		return fmt.Errorf("build auth methods: %w", err)
	}

	sshCfg := &ssh.ClientConfig{
		User:            c.cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Phase 4에서 known_hosts 검증 추가
		Timeout:         c.cfg.Timeout,
	}

	dialer := &net.Dialer{Timeout: c.cfg.Timeout}
	netConn, err := dialer.DialContext(ctx, "tcp", c.cfg.Addr())
	if err != nil {
		return fmt.Errorf("tcp dial %s: %w", c.cfg.Addr(), err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, c.cfg.Addr(), sshCfg)
	if err != nil {
		netConn.Close()
		return fmt.Errorf("ssh handshake %s: %w", c.cfg.Addr(), err)
	}

	c.conn = ssh.NewClient(sshConn, chans, reqs)
	c.connected = true

	kaCtx, kaCancel := context.WithCancel(context.Background())
	c.stopKA = kaCancel
	go c.keepalive(kaCtx)

	slog.Info("ssh connected", "host", c.cfg.Host, "user", c.cfg.User)
	return nil
}

// buildAuthMethods는 설정에 따라 SSH 인증 방법 목록을 구성한다.
func (c *Client) buildAuthMethods() ([]ssh.AuthMethod, error) {
	switch c.cfg.AuthType {
	case "key":
		keyData, err := os.ReadFile(c.cfg.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("read ssh key %s: %w", c.cfg.KeyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("parse ssh key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	case "password":
		return []ssh.AuthMethod{ssh.Password(c.cfg.Password)}, nil
	default:
		return nil, fmt.Errorf("unknown auth type: %s", c.cfg.AuthType)
	}
}

// keepalive는 주기적으로 keep-alive 요청을 보내 연결을 유지한다.
func (c *Client) keepalive(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.KeepAlive)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				return
			}
			_, _, err := conn.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				slog.Debug("ssh keepalive failed, marking disconnected", "host", c.cfg.Host)
				c.markDisconnected()
				return
			}
		}
	}
}

// markDisconnected는 연결 상태를 끊긴 것으로 표시한다.
func (c *Client) markDisconnected() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
}
