package sprout

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"

	"git.sr.ht/~whereswaldon/forest-go"
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
	messageID = s.nextMessageID
	s.nextMessageID++
	return s.writeMessageWithID(messageID, errorCtx, format, fmtArgs...)
}

func (s *SproutConn) writeMessageWithID(messageIDIn MessageID, errorCtx, format string, fmtArgs ...interface{}) (messageID MessageID, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf(errorCtx, err)
		}
	}()
	opts := make([]interface{}, 1, len(fmtArgs)+1)
	opts[0] = messageIDIn
	opts = append(opts, fmtArgs...)
	messageID = messageIDIn
	_, err = fmt.Fprintf(s, format, opts...)
	return messageID, err
}

func (s *SproutConn) SendVersion() (MessageID, error) {
	return s.writeMessage("failed to send version: %v", "version %d %d.%d\n", s.Major, s.Minor)
}

func (s *SproutConn) SendQueryAny(nodeType fields.NodeType, quantity int) (MessageID, error) {
	return s.writeMessage("failed to send query_any: %v", "query_any %d %d %d\n", nodeType, quantity)
}

func (s *SproutConn) SendQuery(nodeIds ...*fields.QualifiedHash) (MessageID, error) {
	builder := &strings.Builder{}
	for _, nodeId := range nodeIds {
		b, _ := nodeId.MarshalText()
		_, _ = builder.Write(b)
		builder.WriteString("\n")
	}
	return s.writeMessage("failed to send query: %v", "query %d %d\n%s", len(nodeIds), builder.String())
}

type AncestryRequest struct {
	*fields.QualifiedHash
	Levels int
}

func (r AncestryRequest) String() string {
	b, _ := r.QualifiedHash.MarshalText()
	return fmt.Sprintf("%d %s\n", r.Levels, string(b))
}

func (s *SproutConn) SendAncestry(reqs ...AncestryRequest) (MessageID, error) {
	builder := &strings.Builder{}
	for _, req := range reqs {
		builder.WriteString(req.String())
	}
	return s.writeMessage("failed to send ancestry: %v", "ancestry %d %d\n%s", len(reqs), builder.String())
}

func (s *SproutConn) SendLeavesOf(nodeId *fields.QualifiedHash, quantity int) (MessageID, error) {
	id, _ := nodeId.MarshalText()
	return s.writeMessage("failed to send leaves_of: %v", "leaves_of %d %s %d\n", string(id), quantity)
}

func NodeLine(n forest.Node) string {
	id, _ := n.ID().MarshalText()
	data, _ := n.MarshalBinary()
	return fmt.Sprintf("%s %s\n", string(id), base64.URLEncoding.EncodeToString(data))
}

func (s *SproutConn) SendResponse(msgID MessageID, index int, nodes []forest.Node) (MessageID, error) {
	builder := &strings.Builder{}
	for _, n := range nodes {
		builder.WriteString(NodeLine(n))
	}
	return s.writeMessageWithID(msgID, "failed to send response: %v", "response %d[%d] %d\n%s", index, len(nodes), builder.String())
}
