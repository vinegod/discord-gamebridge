package executor

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHExecutor runs commands on a remote host over SSH.
// Each Send opens a short-lived session and closes it on return —
// the remote process lifecycle is independent of the bot.
type SSHExecutor struct {
	address         string // host:port
	user            string
	signer          ssh.Signer
	hostKeyCallback ssh.HostKeyCallback
}

// NewSSHExecutor creates an executor from file paths resolved from environment variables.
// keyFile is the path to a PEM private key; knownHostsFile is a standard known_hosts file.
func NewSSHExecutor(host string, port int, user, keyFile, knownHostsFile string) (*SSHExecutor, error) {
	keyPEM, err := os.ReadFile(keyFile) // #nosec G304
	if err != nil {
		return nil, fmt.Errorf("read SSH key %q: %w", keyFile, err)
	}

	signer, err := ssh.ParsePrivateKey(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse SSH private key: %w", err)
	}

	cb, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts %q: %w", knownHostsFile, err)
	}

	return &SSHExecutor{
		address:         fmt.Sprintf("%s:%d", host, port),
		user:            user,
		signer:          signer,
		hostKeyCallback: cb,
	}, nil
}

// Send opens an SSH session, runs the command with args, and returns combined output.
// Cancelling ctx closes the connection and interrupts the remote command.
func (e *SSHExecutor) Send(ctx context.Context, command string, args ...string) (string, error) {
	client, err := e.dial(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("SSH new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	type result struct {
		output []byte
		err    error
	}
	done := make(chan result, 1)
	go func() {
		out, err := session.CombinedOutput(shellJoin(command, args))
		done <- result{out, err}
	}()

	select {
	case <-ctx.Done():
		_ = client.Close()
		return "", fmt.Errorf("SSH command timed out: %w", ctx.Err())
	case res := <-done:
		if res.err != nil {
			return string(res.output), fmt.Errorf("SSH command failed: %w", res.err)
		}
		return string(res.output), nil
	}
}

// Healthy returns true if the SSH port is reachable via TCP.
func (e *SSHExecutor) Healthy(ctx context.Context) bool {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", e.address)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (e *SSHExecutor) dial(ctx context.Context) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            e.user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(e.signer)},
		HostKeyCallback: e.hostKeyCallback,
	}

	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", e.address)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", e.address, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, e.address, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH handshake with %s: %w", e.address, err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

// shellJoin builds a shell-safe command string, quoting each argument.
func shellJoin(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, command)
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// shellQuote wraps s in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
