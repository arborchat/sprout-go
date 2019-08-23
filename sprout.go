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
	CurrentMajor = 0
	CurrentMinor = 0
)

type MessageID int

type Verb string

const (
	Version     Verb = "version"
	QueryAny    Verb = "query_any"
	Query       Verb = "query"
	Ancestry    Verb = "ancestry"
	LeavesOf    Verb = "leaves_of"
	Response    Verb = "response"
	Subscribe   Verb = "subscribe"
	Unsubscribe Verb = "unsubscribe"
	Error       Verb = "error"
	ErrorPart   Verb = "error_part"
	OkPart      Verb = "ok_part"
	Announce    Verb = "announce"
)

var formats = map[Verb]string{
	Version:     " %d %d.%d\n",
	QueryAny:    " %d %d %d\n",
	Query:       " %d %d\n",
	Ancestry:    " %d %d\n",
	LeavesOf:    " %d %s %d\n",
	Response:    " %d[%d] %d\n",
	Subscribe:   " %d %d\n",
	Unsubscribe: " %d %d\n",
	Error:       " %d %d\n",
	ErrorPart:   " %d[%d] %d\n",
	OkPart:      " %d[%d] %d\n",
	Announce:    " %d %d\n",
}

type SproutConn struct {
	net.Conn
	Major, Minor  int
	nextMessageID MessageID

	OnVersion func(s *SproutConn, major, minor int) error
}

func New(transport net.Conn) (*SproutConn, error) {
	s := &SproutConn{
		Major:         CurrentMajor,
		Minor:         CurrentMinor,
		nextMessageID: 0,
		Conn:          transport,
	}
	return s, nil
}

func (s *SproutConn) writeMessage(verb Verb, format string, fmtArgs ...interface{}) (messageID MessageID, err error) {
	messageID = s.nextMessageID
	s.nextMessageID++
	return s.writeMessageWithID(messageID, verb, format, fmtArgs...)
}

func (s *SproutConn) writeMessageWithID(messageIDIn MessageID, verb Verb, format string, fmtArgs ...interface{}) (messageID MessageID, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to send %s: %v", string(verb), err)
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
	op := Version
	return s.writeMessage(op, string(op)+formats[op], s.Major, s.Minor)
}

func (s *SproutConn) SendQueryAny(nodeType fields.NodeType, quantity int) (MessageID, error) {
	op := QueryAny
	return s.writeMessage(op, string(op)+formats[op], nodeType, quantity)
}

func (s *SproutConn) SendQuery(nodeIds ...*fields.QualifiedHash) (MessageID, error) {
	builder := &strings.Builder{}
	for _, nodeId := range nodeIds {
		b, _ := nodeId.MarshalText()
		_, _ = builder.Write(b)
		builder.WriteString("\n")
	}
	op := Query
	return s.writeMessage(op, string(op)+formats[op]+"%s", len(nodeIds), builder.String())
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
	op := Ancestry
	return s.writeMessage(op, string(op)+formats[op]+"%s", len(reqs), builder.String())
}

func (s *SproutConn) SendLeavesOf(nodeId *fields.QualifiedHash, quantity int) (MessageID, error) {
	id, _ := nodeId.MarshalText()
	op := LeavesOf
	return s.writeMessage(op, string(op)+formats[op], string(id), quantity)
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
	op := Response
	return s.writeMessageWithID(msgID, op, string(op)+formats[op]+"%s", index, len(nodes), builder.String())
}

func (s *SproutConn) subscribeOp(op Verb, communities []*forest.Community) (MessageID, error) {
	builder := &strings.Builder{}
	for _, community := range communities {
		id, _ := community.ID().MarshalText()
		builder.WriteString(string(id))
		builder.WriteString("\n")
	}
	return s.writeMessage(op, string(op)+formats[op]+"%s", len(communities), builder.String())
}

func (s *SproutConn) SendSubscribe(communities []*forest.Community) (MessageID, error) {
	return s.subscribeOp(Subscribe, communities)
}

func (s *SproutConn) SendUnsubscribe(communities []*forest.Community) (MessageID, error) {
	return s.subscribeOp(Unsubscribe, communities)
}

type ErrorCode int

const (
	ErrorMalformed ErrorCode = iota
)

func (s *SproutConn) SendError(targetMessageID MessageID, errorCode ErrorCode) (MessageID, error) {
	op := Error
	return s.writeMessageWithID(targetMessageID, op, string(op)+formats[op], errorCode)
}

func (s *SproutConn) SendErrorPart(targetMessageID MessageID, index int, errorCode ErrorCode) (MessageID, error) {
	op := ErrorPart
	return s.writeMessageWithID(targetMessageID, op, string(op)+formats[op], index, errorCode)
}

func (s *SproutConn) SendOkPart(targetMessageID MessageID, index int) (MessageID, error) {
	op := OkPart
	return s.writeMessageWithID(targetMessageID, op, string(op)+formats[op], index)
}

func (s *SproutConn) SendAnnounce(nodes []forest.Node) (messageID MessageID, err error) {
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
	op := Announce

	return s.writeMessage(op, string(op)+formats[op]+"%s", len(nodes), builder.String())
}

func (s *SproutConn) readMessage() {
	var word string
	n, err := fmt.Fscanf(s.Conn, "%s", &word)
	if err != nil {
		//todo
	} else if n < 1 {
		//todo
	}
	switch Verb(word) {
	case Version:
		minor, major := 0, 0
		n, err := fmt.Fscanf(s.Conn, formats[Version], &major, &minor)
		if err != nil {
			//todo
		} else if n < 2 {
			//todo
		}
		if err := s.OnVersion(s, major, minor); err != nil {
			//todo
		}
	}

}
