package mail

import (
	"net"
	"sync"
	"time"
)

// boundedConn caps the deadlines imapclient sets on the connection.
//
// imapclient manages its own read and write deadlines (30 s per response, 5
// minutes per literal) and resets them before each command, which would
// override a deadline set from outside. boundedConn applies the earlier of
// imapclient's deadline and our limit, so the per-operation timeout and
// context cancellation always win. tls.Conn forwards deadlines to the
// underlying connection, so this keeps working after STARTTLS.
type boundedConn struct {
	net.Conn

	mu          sync.Mutex
	limit       time.Time // zero: no limit
	read, write time.Time // last deadlines requested by imapclient
}

func (c *boundedConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func (c *boundedConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.read = t
	return c.Conn.SetReadDeadline(c.effective(t))
}

func (c *boundedConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.write = t
	return c.Conn.SetWriteDeadline(c.effective(t))
}

// setLimit sets the outer limit (zero removes it) and applies it at once.
func (c *boundedConn) setLimit(limit time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = limit
	_ = c.Conn.SetReadDeadline(c.effective(c.read))   // a failure surfaces on the next read
	_ = c.Conn.SetWriteDeadline(c.effective(c.write)) // a failure surfaces on the next write
}

func (c *boundedConn) effective(t time.Time) time.Time {
	if c.limit.IsZero() {
		return t
	}
	if t.IsZero() || t.After(c.limit) {
		return c.limit
	}
	return t
}
