// Package mail is the IMAP side of the server: it talks to Proton Mail
// Bridge over STARTTLS on localhost. It knows nothing about MCP.
//
// One connection is shared and guarded by a mutex: an IMAP connection has a
// single selected folder, so commands cannot interleave. The connection is
// opened on first use and reopened after a network error.
//
// Read operations open folders with EXAMINE (read-only) and fetch with
// BODY.PEEK, so reading never marks a message as read.
package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"os"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// DefaultTimeout bounds every IMAP round trip. Bridge on localhost answers in
// milliseconds; a hang means something is wrong.
const DefaultTimeout = 30 * time.Second

// Options configures a Client.
type Options struct {
	Addr      string // host:port of Bridge's IMAP server
	User      string
	Password  config.Secret
	TLSConfig *tls.Config
	Timeout   time.Duration // per operation; DefaultTimeout if zero
	Logger    *slog.Logger  // slog.Default() if nil
}

// Client is a lazily connected IMAP client. It is safe for concurrent use.
type Client struct {
	opts Options

	// maxFetch caps the bytes ReadMessage fetches: MaxMessageBytes, lowered
	// only by tests so they needn't move 32 MiB.
	maxFetch int64

	mu   sync.Mutex
	conn *boundedConn
	imap *imapclient.Client
}

// New returns a Client. It does not connect until the first operation.
func New(opts Options) *Client {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Client{opts: opts, maxFetch: MaxMessageBytes}
}

// TLSConfig builds the TLS configuration for a Bridge on host, verifying the
// server against the certificate exported from Bridge.
//
// Verification is never skipped, not even on localhost: when Bridge is not
// running, any local program can listen on its port, and without
// verification it would receive the Bridge password at login.
func TLSConfig(host, certFile string) (*tls.Config, error) {
	if !config.IsLocalHost(host) {
		return nil, fmt.Errorf("refusing non-local Bridge host %q", host)
	}
	if certFile == "" {
		return nil, fmt.Errorf("no Bridge certificate: export it in Bridge (Settings → Advanced → Export TLS certificates) and set %s", config.EnvCert)
	}
	pemBytes, err := os.ReadFile(certFile) //nolint:gosec // G304: the path is the operator's own configured certificate
	if err != nil {
		return nil, fmt.Errorf("read Bridge certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("read Bridge certificate %s: no PEM certificate found", certFile)
	}
	return &tls.Config{RootCAs: pool, ServerName: host, MinVersion: tls.VersionTLS12}, nil
}

// Close logs out and closes the connection, if one is open.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.imap == nil {
		return nil
	}
	c.conn.setLimit(time.Now().Add(5 * time.Second))
	_ = c.imap.Logout().Wait() // best effort: we close the connection either way
	err := c.imap.Close()
	c.imap, c.conn = nil, nil
	return err
}

// do runs fn on a connected client with the operation timeout and ctx
// cancellation applied to the connection.
func (c *Client) do(ctx context.Context, fn func(*imapclient.Client) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	err := c.withLimit(ctx, func() error {
		if c.imap == nil {
			return c.connect(ctx)
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = c.withLimit(ctx, func() error { return fn(c.imap) })
	if err != nil {
		var imapErr *imap.Error
		if !errors.As(err, &imapErr) {
			// Not a server NO/BAD: the connection is in an unknown state.
			c.opts.Logger.Warn("imap connection dropped", "err", err)
			_ = c.imap.Close()
			c.imap, c.conn = nil, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
	}
	return err
}

// withLimit runs fn with the operation timeout, and ctx cancellation, applied
// to the open connection. While connecting there is no connection yet: connect
// sets the limit itself as soon as it has one.
func (c *Client) withLimit(ctx context.Context, fn func() error) error {
	if c.conn == nil {
		return fn()
	}
	conn := c.conn
	conn.setLimit(time.Now().Add(c.opts.Timeout))
	// Cancelling ctx moves the limit into the past, which unblocks a pending
	// read or write.
	stop := context.AfterFunc(ctx, func() { conn.setLimit(time.Unix(1, 0)) })
	defer func() {
		stop()
		conn.setLimit(time.Time{})
	}()
	return fn()
}

func (c *Client) connect(ctx context.Context) error {
	dialer := net.Dialer{Timeout: c.opts.Timeout}
	raw, err := dialer.DialContext(ctx, "tcp", c.opts.Addr)
	if err != nil {
		return fmt.Errorf("connect to Proton Mail Bridge at %s (is Bridge running?): %w", c.opts.Addr, err)
	}
	conn := &boundedConn{Conn: raw}
	conn.setLimit(time.Now().Add(c.opts.Timeout))
	stop := context.AfterFunc(ctx, func() { conn.setLimit(time.Unix(1, 0)) })
	defer stop()

	client, err := imapclient.NewStartTLS(conn, &imapclient.Options{
		TLSConfig:   c.opts.TLSConfig,
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("start TLS with Bridge: %w", err)
	}
	if err := client.Login(c.opts.User, c.opts.Password.Reveal()).Wait(); err != nil {
		_ = client.Close()
		return fmt.Errorf("log in to Bridge as %s (check the Bridge password): %w", c.opts.User, err)
	}
	conn.setLimit(time.Time{})

	c.conn, c.imap = conn, client
	c.opts.Logger.Info("connected to Bridge", "addr", c.opts.Addr)
	return nil
}
