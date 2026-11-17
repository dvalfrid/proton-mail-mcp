package send

import (
	"bufio"
	"bytes"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/textproto"
)

// splitHeader splits a raw message after the blank line that ends its
// header. The header keeps its final line break, and the body is
// everything after the blank line, untouched.
func splitHeader(raw []byte) (head, body []byte, ok bool) {
	for _, sep := range [][]byte{[]byte("\r\n\r\n"), []byte("\n\n")} {
		if i := bytes.Index(raw, sep); i >= 0 {
			return raw[:i+len(sep)], raw[i+len(sep):], true
		}
	}
	return nil, nil, false
}

func bufioReader(b []byte) *bufio.Reader { return bufio.NewReader(bytes.NewReader(b)) }

func messageHeader(h textproto.Header) message.Header { return message.Header{Header: h} }
