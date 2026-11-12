package server

import (
	"errors"
	"fmt"

	"github.com/dvalfrid/proton-mail-mcp/internal/help"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// helpTopics maps the errors the owner can do something about to their
// section in docs/troubleshooting.md. The first match wins.
var helpTopics = []struct {
	err   error
	topic help.Topic
}{
	{mail.ErrNotSetUp, help.NotSetUp},
	{mail.ErrBridgeUnreachable, help.BridgeDown},
	{mail.ErrLoginFailed, help.LoginFailed},
	{mail.ErrCertificate, help.Certificate},
	{mail.ErrTimeout, help.Timeout},
	{mail.ErrUIDValidityChanged, help.Renumbered},
	{mail.ErrInternal, help.Internal},
}

// explain adds the Help link for err's section, if it has one.
func explain(err error) error {
	for _, h := range helpTopics {
		if errors.Is(err, h.err) {
			return fmt.Errorf("%w\nHelp: %s", err, help.URL(h.topic))
		}
	}
	return err
}
