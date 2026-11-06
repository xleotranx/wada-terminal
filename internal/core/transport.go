package core

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

// CommandTransport carries a core through a command's standard input and
// output. It is how a core on another machine is reached: an SSH command runs
// `core-proxy` there, and the proxy speaks to the remote core's socket.
type CommandTransport struct {
	Context context.Context
	Args    []string
	Env     []string
	Stderr  io.Writer
}

// Dial starts the command and returns a connection to its pipes.
func (t CommandTransport) Dial() (net.Conn, error) {
	if len(t.Args) == 0 {
		return nil, fmt.Errorf("core: no command for the transport")
	}
	ctx := t.Context
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, t.Args[0], t.Args[1:]...)
	if len(t.Env) > 0 {
		cmd.Env = t.Env
	}
	if t.Stderr != nil {
		cmd.Stderr = t.Stderr
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	return &pipeConn{in: stdin, out: stdout, cmd: cmd}, nil
}

// pipeConn is a net.Conn over a child process's pipes. Pipe deadlines bound
// requests even when SSH or the remote core stops answering.
type pipeConn struct {
	in       io.WriteCloser
	out      io.ReadCloser
	cmd      *exec.Cmd
	once     sync.Once
	closeErr error
}

func (p *pipeConn) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error) { return p.in.Write(b) }

func (p *pipeConn) Close() error {
	p.once.Do(func() {
		_ = p.in.Close()
		_ = p.out.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		p.closeErr = p.cmd.Wait()
	})
	return p.closeErr
}

func (p *pipeConn) LocalAddr() net.Addr  { return pipeAddr("local") }
func (p *pipeConn) RemoteAddr() net.Addr { return pipeAddr("remote") }
func (p *pipeConn) SetDeadline(at time.Time) error {
	if err := p.SetReadDeadline(at); err != nil {
		return err
	}
	return p.SetWriteDeadline(at)
}
func (p *pipeConn) SetReadDeadline(at time.Time) error {
	if f, ok := p.out.(interface{ SetReadDeadline(time.Time) error }); ok {
		return f.SetReadDeadline(at)
	}
	return os.ErrNoDeadline
}
func (p *pipeConn) SetWriteDeadline(at time.Time) error {
	if f, ok := p.in.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return f.SetWriteDeadline(at)
	}
	return os.ErrNoDeadline
}

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

// Proxy bridges standard input and output to a core's Unix socket. It is the
// remote half of CommandTransport: `wada-terminal core-proxy` runs it over
// SSH, and the local client speaks the ordinary core protocol through it.
func Proxy(dir string, in io.Reader, out io.Writer) error {
	conn, err := net.DialTimeout("unix", SocketPath(dir), 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(conn, in)
		closeRead(conn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(out, conn)
		done <- struct{}{}
	}()
	<-done
	return nil
}

// closeRead ends the socket's read half where the platform supports it, so a
// proxy whose client went away does not wait on the other copy forever.
func closeRead(conn net.Conn) {
	type closeReader interface{ CloseRead() error }
	if c, ok := conn.(closeReader); ok {
		_ = c.CloseRead()
	}
}

// ProxyStdio is Proxy with the process's own streams, which is what the
// hidden core-proxy command calls.
func ProxyStdio(dir string) error { return Proxy(dir, os.Stdin, os.Stdout) }
