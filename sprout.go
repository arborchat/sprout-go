package sprout

import (
	"fmt"
	"net"
	"strings"

	"git.sr.ht/~whereswaldon/forest-go/fields"
)

const (
	major = 0
	minor = 0
)

type MessageID int

type SproutConn struct {
	net.Conn
	Major, Minor  int
	nextMessageID MessageID
}

func (s *SproutConn) writeMessage(errorCtx, format string, fmtArgs ...interface{}) (messageID MessageID, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf(errorCtx, err)
		}
	}()
	messageID = s.nextMessageID
	opts := make([]interface{}, 1, len(fmtArgs)+1)
	opts[0] = messageID
	opts = append(opts, fmtArgs...)
	_, err = fmt.Fprintf(s, format, opts...)
	s.nextMessageID++
	return messageID, err
}

func (s *SproutConn) SendVersion() (messageID MessageID, err error) {
	return s.writeMessage("failed to send version: %v", "version %d %d.%d\n", s.Major, s.Minor)
}

func (s *SproutConn) SendQueryAny(nodeType fields.NodeType, quantity int) (messageID MessageID, err error) {
	return s.writeMessage("failed to send query_any: %v", "query_any %d %d %d\n", nodeType, quantity)
}

func (s *SproutConn) SendQuery(nodeIds []*fields.QualifiedHash) (messageID MessageID, err error) {
	builder := &strings.Builder{}
	for _, nodeId := range nodeIds {
		b, _ := nodeId.MarshalText()
		_, _ = builder.Write(b)
		builder.WriteString("\n")
	}
	return s.writeMessage("failed to send query: %v", "query %d %d\n%s", len(nodeIds), builder.String())
}
