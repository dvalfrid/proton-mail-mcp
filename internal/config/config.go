// Package config loads and validates the server's settings from the
// environment and an optional .env file.
//
// The Bridge password is held as a [Secret], which never prints its value:
// not through fmt, not through log/slog, not through encoding/json. Code that
// needs the value calls [Secret.Reveal], which makes every use easy to find.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
)

// Environment variable names.
const (
	EnvUser      = "PROTON_BRIDGE_USER"
	EnvPassword  = "PROTON_BRIDGE_PASSWORD"
	EnvHost      = "PROTON_BRIDGE_HOST"
	EnvIMAPPort  = "PROTON_BRIDGE_IMAP_PORT"
	EnvSMTPPort  = "PROTON_BRIDGE_SMTP_PORT"
	EnvCert      = "PROTON_BRIDGE_CERT"
	EnvAllowSend = "PROTON_ALLOW_SEND"
	// EnvMaxChanges caps the messages the organize tools change per server
	// run (DefaultMaxChanges if unset): a guard against an assistant fooled
	// by instructions in an email.
	EnvMaxChanges = "PROTON_MAX_CHANGES"
)

// Bridge defaults.
const (
	DefaultHost     = "127.0.0.1"
	DefaultIMAPPort = 1143
	DefaultSMTPPort = 1025
	// DefaultMaxChanges is the default for EnvMaxChanges.
	DefaultMaxChanges = 200
)

// Config is the validated configuration.
type Config struct {
	User      string
	Password  Secret
	Host      string
	IMAPPort  int
	SMTPPort  int
	CertFile  string // PEM certificate exported from Bridge; required, the server is always verified
	AllowSend bool
	// MaxChanges caps the messages changed per server run.
	MaxChanges int
}

// IMAPAddr returns host:port for IMAP.
func (c Config) IMAPAddr() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.IMAPPort)) }

// SMTPAddr returns host:port for SMTP.
func (c Config) SMTPAddr() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.SMTPPort)) }

// LogValue lets a Config be logged safely; the password is redacted by Secret.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("user", c.User),
		slog.String("host", c.Host),
		slog.Int("imap_port", c.IMAPPort),
		slog.Int("smtp_port", c.SMTPPort),
		slog.Bool("cert", c.CertFile != ""),
		slog.Bool("allow_send", c.AllowSend),
		slog.Int("max_changes", c.MaxChanges),
	)
}

// Load builds a Config from lookup, which returns a variable's value and
// whether it is set (os.LookupEnv, or a map in tests).
func Load(lookup func(string) (string, bool)) (Config, error) {
	get := func(key string) string {
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}

	cfg := Config{
		User:     get(EnvUser),
		Password: Secret(get(EnvPassword)),
		Host:     get(EnvHost),
		CertFile: get(EnvCert),
	}
	if cfg.Host == "" {
		cfg.Host = DefaultHost
	}

	var errs []error
	if cfg.User == "" {
		errs = append(errs, fmt.Errorf("%s is not set", EnvUser))
	}
	if cfg.Password == "" {
		errs = append(errs, fmt.Errorf("%s is not set (use the Bridge password, not the Proton account password)", EnvPassword))
	}
	if cfg.CertFile == "" {
		errs = append(errs, fmt.Errorf("%s is not set: export the certificate in Bridge (Settings → Advanced → Export TLS certificates) and give its path", EnvCert))
	}
	if !IsLocalHost(cfg.Host) {
		errs = append(errs, fmt.Errorf("%s=%q: only 127.0.0.1, ::1 or localhost are allowed; Proton Mail Bridge runs on this machine", EnvHost, cfg.Host))
	}

	var err error
	if cfg.IMAPPort, err = port(get(EnvIMAPPort), DefaultIMAPPort, EnvIMAPPort); err != nil {
		errs = append(errs, err)
	}
	if cfg.SMTPPort, err = port(get(EnvSMTPPort), DefaultSMTPPort, EnvSMTPPort); err != nil {
		errs = append(errs, err)
	}

	cfg.MaxChanges = DefaultMaxChanges
	if v := get(EnvMaxChanges); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("%s=%q: give a whole number of messages, at least 1", EnvMaxChanges, v))
		} else {
			cfg.MaxChanges = n
		}
	}
	switch v := get(EnvAllowSend); v {
	case "", "0", "false":
		cfg.AllowSend = false
	case "1", "true":
		cfg.AllowSend = true
	default:
		errs = append(errs, fmt.Errorf("%s=%q: use 1 or 0", EnvAllowSend, v))
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// IsLocalHost reports whether host is the loopback address or "localhost".
func IsLocalHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func port(v string, def int, name string) (int, error) {
	if v == "" {
		return def, nil
	}
	p, err := strconv.Atoi(v)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("%s=%q: not a port number", name, v)
	}
	return p, nil
}

// ReadDotEnv parses a .env file: KEY=VALUE lines, '#' comments, optional
// single or double quotes around the value. A missing file is not an error:
// the settings may come from the environment instead.
func ReadDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the path is the operator's own config file
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }() // read-only file

	vars := map[string]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		if !ok || key == "" {
			// The line is not echoed back: it could hold the password.
			return nil, fmt.Errorf("%s line %d: expected KEY=VALUE", path, n)
		}
		vars[key] = unquote(strings.TrimSpace(value))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return vars, nil
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// Lookup returns a lookup function where the process environment wins over
// values from a .env file.
func Lookup(dotenv map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if v, ok := os.LookupEnv(key); ok {
			return v, true
		}
		v, ok := dotenv[key]
		return v, ok
	}
}
