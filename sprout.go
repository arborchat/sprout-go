package sprout

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"

	forest "git.sr.ht/~whereswaldon/forest-go"
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

func (s *SproutConn) Announce(nodes []forest.Node) (messageID MessageID, err error) {
	builder := &strings.Builder{}
	for _, node := range nodes {
		id := node.ID()
		b, _ := id.MarshalText()
		n, _ := node.MarshalBinary()
		enc := base64.URLEncoding.EncodeToString(n)
		_, _ = builder.Write(b)
		_, _ = builder.WriteString(enc)
		builder.WriteString("\n")
	}

	return s.writeMessage("failed to make announcement: %v", "announce %d %d\n%s", len(nodes), builder.String())
}
