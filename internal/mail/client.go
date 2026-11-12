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
	"encoding/pem"
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

// DefaultSearchTimeout bounds a search. Bridge has no search index: a text
// criterion makes it read every message in the folder, which took 13 s with a
// cold cache in a folder of 30,000 (issue #39). It stays under the 60 s an MCP
// client waits by default, so the error, with its advice, reaches Claude.
const DefaultSearchTimeout = 55 * time.Second

// Errors from connecting, each with what to do about it. The server adds a
// link to the matching section of docs/troubleshooting.md.
var (
	ErrNotSetUp          = errors.New("proton-mail-mcp isn't set up on this computer: open a terminal and run `proton-mail-mcp setup`")
	ErrBridgeUnreachable = errors.New("start Proton Mail Bridge, sign in, and try again; `proton-mail-mcp check` shows what is wrong")
	ErrLoginFailed       = errors.New("the Bridge password may have changed (it does when Bridge is reset): run `proton-mail-mcp setup` in a terminal")
	ErrCertificate       = errors.New("if Bridge was reset or reinstalled, run `proton-mail-mcp setup` again; `proton-mail-mcp check` shows which program holds the port")
)

// ErrInternal means an operation failed on a bug (a panic) rather than on
// Bridge's answer. The connection is dropped; the next call reconnects.
var ErrInternal = errors.New("internal error")

// ErrTimeout means Bridge did not answer within the operation's timeout.
var ErrTimeout = errors.New("no answer from Bridge in time")

// Options configures a Client.
type Options struct {
	Addr      string // host:port of Bridge's IMAP server
	User      string
	Password  config.Secret
	TLSConfig *tls.Config
	Timeout   time.Duration // per operation; DefaultTimeout if zero
	// SearchTimeout replaces Timeout for SearchMessages; DefaultSearchTimeout
	// if zero.
	SearchTimeout time.Duration
	// MaxChanges caps the messages the organize operations change over the
	// Client's life; DefaultMaxChanges if zero.
	MaxChanges int
	Logger     *slog.Logger // slog.Default() if nil
	// Location is the owner's time zone: dates are shown in it and search
	// days are its days. time.Local, the computer's own zone, if nil.
	Location *time.Location
}

// Client is a lazily connected IMAP client. It is safe for concurrent use.
type Client struct {
	opts Options

	// maxFetch caps the bytes ReadMessage fetches: MaxMessageBytes, lowered
	// only by tests so they needn't move 32 MiB.
	maxFetch int64

	// notSetUp, if set, is returned by every operation: see NewUnconfigured.
	notSetUp error

	// changed counts the messages the organize operations were asked to
	// change, against MaxChanges. It has its own lock: it is reserved
	// before an operation takes mu.
	changedMu sync.Mutex
	changed   int

	mu   sync.Mutex
	conn *boundedConn
	imap *imapclient.Client
}

// New returns a Client. It does not connect until the first operation.
func New(opts Options) *Client {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.SearchTimeout == 0 {
		opts.SearchTimeout = DefaultSearchTimeout
	}
	if opts.MaxChanges == 0 {
		opts.MaxChanges = DefaultMaxChanges
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Location == nil {
		opts.Location = time.Local
	}
	return &Client{opts: opts, maxFetch: MaxMessageBytes}
}

// NewUnconfigured returns a Client for a server that has no usable settings.
// Every operation fails with ErrNotSetUp and the reason, so the tools can
// tell the owner to run setup instead of the server not starting at all.
func NewUnconfigured(reason error, logger *slog.Logger) *Client {
	c := New(Options{Logger: logger})
	c.notSetUp = fmt.Errorf("%w (%w)", ErrNotSetUp, reason)
	return c
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
	cfg, err := TLSConfigFromPEM(host, pemBytes)
	if err != nil {
		return nil, fmt.Errorf("read Bridge certificate %s: %w", certFile, err)
	}
	return cfg, nil
}

// TLSConfigFromPEM is TLSConfig with the certificate given as PEM, for setup,
// which tests the login before it writes the certificate to disk.
func TLSConfigFromPEM(host string, pemBytes []byte) (*tls.Config, error) {
	if !config.IsLocalHost(host) {
		return nil, fmt.Errorf("refusing non-local Bridge host %q", host)
	}
	cert, err := oneCertificate(pemBytes)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{RootCAs: pool, ServerName: host, MinVersion: tls.VersionTLS12}, nil
}

// oneCertificate returns the single certificate in pemBytes. Anything else
// is refused: trusting every certificate in a bundle would trust whatever a
// bundled CA signs for 127.0.0.1, while check compares only the first
// (issue #75). A private key block (Bridge's export writes key.pem next to
// cert.pem) is refused too.
func oneCertificate(pemBytes []byte) (*x509.Certificate, error) {
	var found []*pem.Block
	for rest := pemBytes; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("the certificate file holds a %q block; it must hold only Bridge's certificate", block.Type)
		}
		found = append(found, block)
	}
	switch len(found) {
	case 0:
		return nil, errors.New("no PEM certificate found")
	case 1:
		return x509.ParseCertificate(found[0].Bytes)
	}
	return nil, fmt.Errorf("the certificate file holds %d certificates; it must hold only Bridge's", len(found))
}

// Ping connects and logs in if needed, then sends NOOP: it proves that the
// pinned certificate and the password both work.
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, func(ic *imapclient.Client) error { return ic.Noop().Wait() })
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
	return c.doFor(ctx, c.opts.Timeout, fn)
}

// doFor is do with another timeout, which also bounds connecting. A timeout
// is reported as ErrTimeout.
func (c *Client) doFor(ctx context.Context, timeout time.Duration, fn func(*imapclient.Client) error) error {
	limit := time.Now().Add(timeout)
	err := c.run(ctx, timeout, fn)
	// The client library doesn't always wrap the deadline error (it formats
	// some with %v), so a failure once the limit has passed counts too, unless
	// it is the server's own NO or BAD answer.
	var imapErr *imap.Error
	if err != nil && ctx.Err() == nil && !errors.As(err, &imapErr) &&
		(errors.Is(err, os.ErrDeadlineExceeded) || !time.Now().Before(limit)) {
		return fmt.Errorf("%w (%v): %w", ErrTimeout, timeout, err)
	}
	return err
}

func (c *Client) run(ctx context.Context, timeout time.Duration, fn func(*imapclient.Client) error) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Runs before the unlock: a panic in an operation or in the IMAP library
	// becomes an error, and the connection, now in an unknown state, is
	// dropped while the lock is still held.
	defer func() {
		if r := recover(); r != nil {
			LogPanic(c.opts.Logger, "imap operation", r)
			if c.imap != nil {
				_ = c.imap.Close()
			}
			c.imap, c.conn = nil, nil
			err = ErrInternal
		}
	}()

	if err := ctx.Err(); err != nil {
		return err
	}
	if c.notSetUp != nil {
		return c.notSetUp
	}
	err = c.withLimit(ctx, timeout, func() error {
		if c.imap == nil {
			return c.connect(ctx, timeout)
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = c.withLimit(ctx, timeout, func() error { return fn(c.imap) })
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
func (c *Client) withLimit(ctx context.Context, timeout time.Duration, fn func() error) error {
	if c.conn == nil {
		return fn()
	}
	conn := c.conn
	conn.setLimit(time.Now().Add(timeout))
	// Cancelling ctx moves the limit into the past, which unblocks a pending
	// read or write.
	stop := context.AfterFunc(ctx, func() { conn.setLimit(time.Unix(1, 0)) })
	defer func() {
		stop()
		conn.setLimit(time.Time{})
	}()
	return fn()
}

func (c *Client) connect(ctx context.Context, timeout time.Duration) error {
	dialer := net.Dialer{Timeout: timeout}
	raw, err := dialer.DialContext(ctx, "tcp", c.opts.Addr)
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		// The dial ran out of time (on Linux this isn't os.ErrDeadlineExceeded):
		// a timeout, not a Bridge that isn't running.
		return fmt.Errorf("connect to Proton Mail Bridge on %s: %w", c.opts.Addr, err)
	}
	if err != nil {
		return fmt.Errorf("no answer from Proton Mail Bridge on %s: %w (%w)", c.opts.Addr, ErrBridgeUnreachable, err)
	}
	conn := &boundedConn{Conn: raw}
	conn.setLimit(time.Now().Add(timeout))
	stop := context.AfterFunc(ctx, func() { conn.setLimit(time.Unix(1, 0)) })
	defer stop()

	client, err := imapclient.NewStartTLS(conn, &imapclient.Options{
		TLSConfig:   c.opts.TLSConfig,
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	})
	if err != nil {
		_ = conn.Close()
		var verr *tls.CertificateVerificationError
		if errors.As(err, &verr) {
			return fmt.Errorf("the server at %s does not present the pinned Bridge certificate: %w (%w)", c.opts.Addr, ErrCertificate, err)
		}
		return fmt.Errorf("start TLS with Bridge: %w", err)
	}
	if err := client.Login(c.opts.User, c.opts.Password.Reveal()).Wait(); err != nil {
		_ = client.Close()
		return fmt.Errorf("login refused by Proton Mail Bridge for %s: %w (%w)", c.opts.User, ErrLoginFailed, err)
	}
	conn.setLimit(time.Time{})

	c.conn, c.imap = conn, client
	c.opts.Logger.Info("connected to Bridge", "addr", c.opts.Addr)
	return nil
}
