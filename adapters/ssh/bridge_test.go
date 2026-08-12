package stavessh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	charmssh "charm.land/ssh"
	wish "charm.land/wish/v2"
	"github.com/ben-ranford/stave/capability"
	gossh "golang.org/x/crypto/ssh"
)

func TestSSHTransportContract_MultiClientIsolation(t *testing.T) {
	t.Parallel()
	requireNoRacePTY(t)

	factory := newFakeFactory()
	server := newWishServer(t, Middleware(factory))

	alpha := openPTYSession(t, server, sessionConfig{env: map[string]string{"TERM": "xterm-256color"}, width: 80, height: 24})
	beta := openPTYSession(t, server, sessionConfig{env: map[string]string{"TERM": "dumb"}, width: 40, height: 12})

	alpha.write("alpha\n")
	beta.write("beta\n")
	alpha.closeStdin()
	beta.closeStdin()

	alpha.wait()
	beta.wait()

	if strings.Contains(alpha.stdout.String(), "beta") {
		t.Fatalf("alpha stdout leaked beta data: %q", alpha.stdout.String())
	}
	if strings.Contains(beta.stdout.String(), "alpha") {
		t.Fatalf("beta stdout leaked alpha data: %q", beta.stdout.String())
	}

	reqs := factory.requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(reqs))
	}
	if reqs[0].ID == reqs[1].ID {
		t.Fatalf("session ids should be unique: %q", reqs[0].ID)
	}
	if reqs[0].Manifest.Color == reqs[1].Manifest.Color {
		t.Fatalf("capabilities should differ per client: %+v %+v", reqs[0].Manifest, reqs[1].Manifest)
	}
}

func TestSSHTransportContract_ResizeAndInput(t *testing.T) {
	t.Parallel()
	requireNoRacePTY(t)

	factory := newFakeFactory()
	server := newRawServer(t, Handler(factory))

	client := openPTYSession(t, server, sessionConfig{
		env:    map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"},
		width:  90,
		height: 30,
	})

	client.write("hello\n")
	client.windowChange(33, 120)
	waitForSubstring(t, client.stdout, "resize=120x33")
	client.closeStdin()
	client.wait()

	got := client.stdout.String()
	if !strings.Contains(got, "input=hello") {
		t.Fatalf("missing echoed input in %q", got)
	}
	if !strings.Contains(got, "resize=120x33") {
		t.Fatalf("missing resize event in %q", got)
	}
}

func TestSSHTransportPerformanceBudget(t *testing.T) {
	requireNoRacePTY(t)
	factory := newFakeFactory()
	server := newRawServer(t, Handler(factory))
	client := openPTYSession(t, server, sessionConfig{
		env:    map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"},
		width:  120,
		height: 40,
	})

	const samples = 21
	latencies := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		marker := fmt.Sprintf("perf-%02d", i)
		start := time.Now()
		client.write(marker + "\n")
		waitForSubstring(t, client.stdout, "input="+marker)
		latencies = append(latencies, time.Since(start))
	}
	client.closeStdin()
	client.wait()
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p95 := latencies[(len(latencies)*95+99)/100-1]
	t.Logf("live loopback SSH input-to-output p95=%s samples=%d", p95, samples)
	if p95 > 75*time.Millisecond {
		t.Fatalf("live SSH p95 %s exceeds 75ms budget", p95)
	}
}

func TestSSHTransportContract_CancellationOnDisconnect(t *testing.T) {
	t.Parallel()
	requireNoRacePTY(t)

	factory := newFakeFactory()
	server := newRawServer(t, Handler(factory))

	client := openPTYSession(t, server, sessionConfig{
		env:    map[string]string{"TERM": "xterm-256color"},
		width:  80,
		height: 24,
	})
	client.write("stay-open")

	if err := client.session.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}
	_ = client.conn.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sessions := factory.sessions()
		if len(sessions) == 1 && sessions[0].cancelled() && sessions[0].restored() && sessions[0].closed() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	sessions := factory.sessions()
	if len(sessions) != 1 {
		t.Fatalf("expected exactly one fake session, got %d", len(sessions))
	}
	t.Fatalf("disconnect did not cancel and cleanup session: cancelled=%v restored=%v closed=%v", sessions[0].cancelled(), sessions[0].restored(), sessions[0].closed())
}

func TestSSHTransportContract_ProfileNegotiationAndNoninteractiveFallback(t *testing.T) {
	t.Parallel()
	requireNoRacePTY(t)

	factory := newFakeFactory()
	server := newRawServer(t, Handler(factory))

	interactive := openPTYSession(t, server, sessionConfig{
		env:    map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"},
		width:  100,
		height: 40,
	})
	interactive.closeStdin()
	interactive.wait()

	plainOut, plainErr := runExecSession(t, server, sessionConfig{
		env:     map[string]string{"TERM": "dumb", "NO_COLOR": "1"},
		command: "status",
	})
	if plainErr != "" {
		t.Fatalf("unexpected exec stderr: %q", plainErr)
	}
	if !strings.Contains(plainOut, "noninteractive=true") {
		t.Fatalf("expected noninteractive marker, got %q", plainOut)
	}

	reqs := factory.requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(reqs))
	}
	var ttyReq, plainReq OpenRequest
	for _, req := range reqs {
		if req.Manifest.TTY {
			ttyReq = req
		} else {
			plainReq = req
		}
	}
	if ttyReq.Manifest.Color != capability.ColorTrueColor {
		t.Fatalf("expected truecolor interactive manifest, got %+v", ttyReq.Manifest)
	}
	if plainReq.Manifest.TTY || plainReq.Manifest.Interactive {
		t.Fatalf("expected noninteractive plain manifest, got %+v", plainReq.Manifest)
	}
	if plainReq.Manifest.OutputMode != capability.OutputPlain {
		t.Fatalf("expected plain output mode, got %+v", plainReq.Manifest)
	}
}

func TestSSHTransportContract_NoSecretDiagnosticLeakage(t *testing.T) {
	t.Parallel()
	requireNoRacePTY(t)

	factory := newFakeFactory()
	server := newRawServer(t, Handler(factory, WithLimits(Limits{
		MaxEnvironmentEntries: defaultMaxEnvironmentEntries,
		MaxEnvironmentBytes:   defaultMaxEnvironmentBytes,
		MaxCommandBytes:       defaultMaxCommandBytes,
		MaxInputBytes:         4,
		MaxInputChunkBytes:    4,
		MaxOutputBytes:        defaultMaxOutputBytes,
		MaxDiagnosticBytes:    defaultMaxDiagnosticBytes,
		MaxTreeNodes:          defaultMaxTreeNodes,
	})))

	client := openPTYSession(t, server, sessionConfig{
		env:    map[string]string{"TERM": "xterm-256color", "API_KEY": "super-secret-token"},
		width:  80,
		height: 24,
	})
	client.write("secret-token")
	client.closeStdin()
	client.wait()

	if strings.Contains(client.stderr.String(), "secret-token") || strings.Contains(client.stderr.String(), "super-secret-token") {
		t.Fatalf("stderr leaked secret material: %q", client.stderr.String())
	}
	if strings.Contains(client.stdout.String(), "[error]") {
		t.Fatalf("diagnostics leaked to stdout: %q", client.stdout.String())
	}
	if !strings.Contains(client.stderr.String(), "RESOURCE_LIMIT") {
		t.Fatalf("expected resource limit diagnostic, got %q", client.stderr.String())
	}
}

func TestSSHTransportContract_NoninteractiveFallback(t *testing.T) {
	t.Parallel()

	factory := newFakeFactory()
	server := newRawServer(t, Handler(factory))

	stdout, stderr := runExecSession(t, server, sessionConfig{
		env:     map[string]string{"TERM": "dumb", "NO_COLOR": "1"},
		command: "status",
	})
	if stderr != "" {
		t.Fatalf("unexpected exec stderr: %q", stderr)
	}
	if !strings.Contains(stdout, "noninteractive=true") {
		t.Fatalf("expected noninteractive marker, got %q", stdout)
	}
	reqs := factory.requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Manifest.TTY || reqs[0].Manifest.Interactive || reqs[0].Manifest.OutputMode != capability.OutputPlain {
		t.Fatalf("unexpected noninteractive manifest: %+v", reqs[0].Manifest)
	}
}

type fakeFactory struct {
	mu   sync.Mutex
	reqs []OpenRequest
	sess []*fakeSession
}

func newFakeFactory() *fakeFactory {
	return &fakeFactory{}
}

func (f *fakeFactory) Open(_ context.Context, req OpenRequest) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	session := newFakeSession(req)
	f.reqs = append(f.reqs, cloneRequest(req))
	f.sess = append(f.sess, session)
	return session, nil
}

func (f *fakeFactory) requests() []OpenRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]OpenRequest, len(f.reqs))
	copy(out, f.reqs)
	return out
}

func (f *fakeFactory) sessions() []*fakeSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*fakeSession, len(f.sess))
	copy(out, f.sess)
	return out
}

type fakeSession struct {
	req       OpenRequest
	done      chan error
	once      sync.Once
	mu        sync.Mutex
	restored0 bool
	closed0   bool
	cancel0   bool
}

func newFakeSession(req OpenRequest) *fakeSession {
	s := &fakeSession{
		req:  req,
		done: make(chan error, 1),
	}
	fmt.Fprintf(req.Output, "session=%s tty=%t noninteractive=%t color=%s initial=%dx%d\n",
		req.ID,
		req.Manifest.TTY,
		!req.Manifest.Interactive,
		req.Manifest.Color.String(),
		req.InitialWindow.Width,
		req.InitialWindow.Height,
	)
	return s
}

func (s *fakeSession) Dispatch(_ context.Context, ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Kind {
	case KindText:
		chunk := ev.Payload.(InputChunk)
		text := strings.TrimSpace(string(chunk.Data))
		if text != "" {
			fmt.Fprintf(s.req.Output, "session=%s input=%s\n", s.req.ID, text)
		}
	case KindResize:
		resize := ev.Payload.(Resize)
		fmt.Fprintf(s.req.Output, "session=%s resize=%dx%d\n", s.req.ID, resize.Window.Width, resize.Window.Height)
	case KindCancel:
		s.cancel0 = true
		s.finish(nil)
	case KindShutdown:
		s.finish(nil)
	}
	return nil
}

func (s *fakeSession) Wait() error {
	return <-s.done
}

func (s *fakeSession) Restore(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restored0 = true
	return nil
}

func (s *fakeSession) Close() error {
	s.mu.Lock()
	s.closed0 = true
	s.mu.Unlock()
	s.finish(nil)
	return nil
}

func (s *fakeSession) finish(err error) {
	s.once.Do(func() {
		s.done <- err
		close(s.done)
	})
}

func (s *fakeSession) restored() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restored0
}

func (s *fakeSession) closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed0
}

func (s *fakeSession) cancelled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel0
}

func cloneRequest(req OpenRequest) OpenRequest {
	copyReq := req
	copyReq.Command = append([]string(nil), req.Command...)
	copyReq.Environment = make(map[string]string, len(req.Environment))
	for key, value := range req.Environment {
		copyReq.Environment[key] = value
	}
	copyReq.Output = nil
	copyReq.Diagnostics = nil
	return copyReq
}

type sessionConfig struct {
	env     map[string]string
	width   int
	height  int
	command string
}

type liveSession struct {
	conn    *gossh.Client
	session *gossh.Session
	stdin   io.WriteCloser
	stdout  *syncBuffer
	stderr  *syncBuffer
	waitCh  chan error
}

func openPTYSession(t *testing.T, addr string, cfg sessionConfig) *liveSession {
	t.Helper()
	conn, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            "testuser",
		Auth:            []gossh.AuthMethod{gossh.Password("testpass")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         3 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial ssh: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	session, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	for key, value := range cfg.env {
		if err := session.Setenv(key, value); err != nil {
			t.Fatalf("setenv %s: %v", key, err)
		}
	}
	if err := session.RequestPty(cfg.env["TERM"], cfg.height, cfg.width, gossh.TerminalModes{}); err != nil {
		t.Fatalf("request pty: %v", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout := &syncBuffer{}
	stderr := &syncBuffer{}
	session.Stdout = stdout
	session.Stderr = stderr

	if err := session.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- session.Wait()
		close(waitCh)
	}()

	return &liveSession{
		conn:    conn,
		session: session,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		waitCh:  waitCh,
	}
}

func (s *liveSession) write(value string) {
	_, _ = io.WriteString(s.stdin, value)
}

func (s *liveSession) closeStdin() {
	_ = s.stdin.Close()
}

func (s *liveSession) wait() {
	if err := <-s.waitCh; err != nil {
		var exitErr *gossh.ExitError
		if errors.As(err, &exitErr) {
			return
		}
	}
}

func (s *liveSession) windowChange(height, width int) {
	_ = s.session.WindowChange(height, width)
}

func waitForSubstring(t *testing.T, buffer *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buffer.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in %q", want, buffer.String())
}

func runExecSession(t *testing.T, addr string, cfg sessionConfig) (string, string) {
	t.Helper()
	conn, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            "testuser",
		Auth:            []gossh.AuthMethod{gossh.Password("testpass")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         3 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial ssh: %v", err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()

	for key, value := range cfg.env {
		if err := session.Setenv(key, value); err != nil {
			t.Fatalf("setenv %s: %v", key, err)
		}
	}

	stdout := &syncBuffer{}
	stderr := &syncBuffer{}
	session.Stdout = stdout
	session.Stderr = stderr
	_ = session.Run(cfg.command)
	return stdout.String(), stderr.String()
}

func TestLockedWriterAllowsSGRAndRejectsTerminalControlProtocols(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "plain", input: "hello\n"},
		{name: "sgr", input: "\x1b[38;2;255;0;0mred\x1b[0m\n"},
		{name: "osc clipboard", input: "\x1b]52;c;c2VjcmV0\a", wantErr: true},
		{name: "osc title", input: "\x1b]0;owned\a", wantErr: true},
		{name: "cursor movement", input: "\x1b[2J", wantErr: true},
		{name: "device control", input: "\x1bPpayload\x1b\\", wantErr: true},
		{name: "c1", input: "hello\u009b31m", wantErr: true},
		{name: "incomplete", input: "hello\x1b", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dst bytes.Buffer
			writer := newLockedWriter(&dst, 4096)
			_, err := writer.Write([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Write error=%v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr && dst.Len() != 0 {
				t.Fatalf("unsafe output was partially written: %q", dst.String())
			}
		})
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func requireNoRacePTY(t *testing.T) {
	t.Helper()
	if raceEnabled {
		t.Skip("PTY integration is skipped under -race because charm.land/ssh v0.4.2 exposes a session.Pty() race during window-state mutation")
	}
}

func newWishServer(t *testing.T, middleware wish.Middleware) string {
	t.Helper()
	hostKey := testHostKeyPEM(t)
	server, err := wish.NewServer(
		wish.WithHostKeyPEM(hostKey),
		wish.WithMiddleware(middleware),
	)
	if err != nil {
		t.Fatalf("new wish server: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, charmssh.ErrServerClosed) && !isClosedListenerError(err) {
			t.Errorf("serve ssh: %v", err)
		}
	}()
	return listener.Addr().String()
}

func newRawServer(t *testing.T, handler charmssh.Handler) string {
	t.Helper()
	hostKey := testHostKeyPEM(t)
	server := &charmssh.Server{Handler: handler}
	if err := server.SetOption(charmssh.HostKeyPEM(hostKey)); err != nil {
		t.Fatalf("set host key: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, charmssh.ErrServerClosed) && !isClosedListenerError(err) {
			t.Errorf("serve raw ssh: %v", err)
		}
	}()
	return listener.Addr().String()
}

func isClosedListenerError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "use of closed network connection")
}

func testHostKeyPEM(t *testing.T) []byte {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	block, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal host key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: block})
}
