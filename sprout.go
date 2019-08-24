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

	OnVersion     func(s *SproutConn, messageID MessageID, major, minor int) error
	OnQueryAny    func(s *SproutConn, messageID MessageID, nodeType fields.NodeType, quantity int) error
	OnQuery       func(s *SproutConn, messageID MessageID, nodeIds []*fields.QualifiedHash) error
	OnAncestry    func(s *SproutConn, messageID MessageID, ancestryRequests []AncestryRequest) error
	OnLeavesOf    func(s *SproutConn, messageID MessageID, nodeID *fields.QualifiedHash, quantity int) error
	OnResponse    func(s *SproutConn, targetMessageID MessageID, targetIndex int, nodes []forest.Node) error
	OnSubscribe   func(s *SproutConn, messageID MessageID, nodeIds []*fields.QualifiedHash) error
	OnUnsubscribe func(s *SproutConn, messageID MessageID, nodeIds []*fields.QualifiedHash) error
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

const ancestryRequestLinePattern = "%d %s\n"

func (r AncestryRequest) String() string {
	b, _ := r.QualifiedHash.MarshalText()
	return fmt.Sprintf(ancestryRequestLinePattern, r.Levels, string(b))
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

const nodeLineFormat = "%s %s\n"

func NodeLine(n forest.Node) string {
	id, _ := n.ID().MarshalText()
	data, _ := n.MarshalBinary()
	return fmt.Sprintf(nodeLineFormat, string(id), base64.URLEncoding.EncodeToString(data))
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

func (s *SproutConn) scanOp(verb Verb, fields ...interface{}) error {
	n, err := fmt.Fscanf(s.Conn, formats[verb], fields...)
	if err != nil {
		return fmt.Errorf("failed to scan %s: %v", verb, err)
	} else if n < len(fields) {
		return fmt.Errorf("failed to scan enough arguments for %s (got %d, expected %d)", verb, n, len(fields))
	}
	return nil
}

func (s *SproutConn) readMessage() error {
	var word string
	n, err := fmt.Fscanf(s.Conn, "%s", &word)
	if err != nil {
		return fmt.Errorf("error scanning verb: %v", err)
	} else if n < 1 {
		return fmt.Errorf("failed to read a verb")
	}
	verb := Verb(word)
	switch verb {
	case Version:
		var (
			major, minor int
			messageID    MessageID
		)
		if err := s.scanOp(verb, &messageID, &major, &minor); err != nil {
			return err
		}
		if err := s.OnVersion(s, messageID, major, minor); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case QueryAny:
		var (
			messageID MessageID
			nodeType  fields.NodeType
			quantity  int
		)
		if err := s.scanOp(verb, &messageID, &nodeType, &quantity); err != nil {
			return err
		}
		if err := s.OnQueryAny(s, messageID, nodeType, quantity); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case Query:
		var (
			messageID MessageID
			count     int
		)
		if err := s.scanOp(verb, &messageID, &count); err != nil {
			return err
		}
		ids, err := s.readNodeIDs(count)
		if err != nil {
			return fmt.Errorf("failed to read node ids in query message: %v", err)
		}
		if err := s.OnQuery(s, messageID, ids); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case Ancestry:
		var (
			messageID MessageID
			count     int
		)
		if err := s.scanOp(verb, &messageID, &count); err != nil {
			return err
		}
		ancestryRequests := make([]AncestryRequest, count)
		for i := 0; i < count; i++ {
			var (
				idString string
				depth    int
			)
			n, err := fmt.Fscanf(s.Conn, ancestryRequestLinePattern, &depth, &idString)
			if err != nil {
				return fmt.Errorf("error reading ancestry request line: %v", err)
			} else if n != 2 {
				return fmt.Errorf("unexpected number of items, expected %d found %d", 2, n)
			}
			id := &fields.QualifiedHash{}
			if err := id.UnmarshalText([]byte(idString)); err != nil {
				return fmt.Errorf("failed to unmarshal ancestry request line: %v", err)
			}
			ancestryRequests[i] = AncestryRequest{
				QualifiedHash: id,
				Levels:        depth,
			}
		}
		if err := s.OnAncestry(s, messageID, ancestryRequests); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case LeavesOf:
		var (
			messageID    MessageID
			nodeIDString string
			quantity     int
		)
		if err := s.scanOp(verb, &messageID, &nodeIDString, &quantity); err != nil {
			return err
		}
		id := &fields.QualifiedHash{}
		if err := id.UnmarshalText([]byte(nodeIDString)); err != nil {
			return fmt.Errorf("failed to unmarshal leave_of target: %v", err)
		}
		if err := s.OnLeavesOf(s, messageID, id, quantity); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case Response:
		var (
			targetMessageID MessageID
			index, count    int
		)
		if err := s.scanOp(verb, &targetMessageID, &index, &count); err != nil {
			return err
		}
		nodes := make([]forest.Node, count)
		for i := 0; i < count; i++ {
			var (
				idString   string
				nodeString string
			)
			n, err := fmt.Fscanf(s.Conn, nodeLineFormat, &idString, &nodeString)
			if err != nil {
				return fmt.Errorf("error reading ancestry request line: %v", err)
			} else if n != 2 {
				return fmt.Errorf("unexpected number of items, expected %d found %d", 2, n)
			}
			id := &fields.QualifiedHash{}
			if err := id.UnmarshalText([]byte(idString)); err != nil {
				return fmt.Errorf("failed to unmarshal ancestry request line: %v", err)
			}
			node, err := NodeFromBase64(nodeString)
			if err != nil {
				return fmt.Errorf("failed to read node from response: %v", err)
			}
			if node.ID() != id {
				expectedIDString, _ := id.MarshalText()
				actualIDString, _ := node.ID().MarshalText()
				return fmt.Errorf("message id mismatch, node given as %s hashes to %s", expectedIDString, actualIDString)
			}
			nodes[i] = node
		}
		if err := s.OnResponse(s, targetMessageID, index, nodes); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case Subscribe:
		fallthrough
	case Unsubscribe:
		var (
			messageID MessageID
			count     int
		)
		if err := s.scanOp(verb, &messageID, &count); err != nil {
			return err
		}

		ids, err := s.readNodeIDs(count)
		if err != nil {
			return fmt.Errorf("failed to read community ids in %s message: %v", string(verb), err)
		}
		hook := s.OnSubscribe
		if verb == Unsubscribe {
			hook = s.OnUnsubscribe
		}
		if err := hook(s, messageID, ids); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	}
	return nil
}

func (s *SproutConn) readNodeIDs(count int) ([]*fields.QualifiedHash, error) {
	ids := make([]*fields.QualifiedHash, count)
	for i := 0; i < count; i++ {
		var idString string
		n, err := fmt.Fscanln(s.Conn, &idString)
		if err != nil {
			return nil, fmt.Errorf("error reading id line: %v", err)
		} else if n != 1 {
			return nil, fmt.Errorf("unexpected number of items, expected %d found %d", 1, n)
		}
		id := &fields.QualifiedHash{}
		if err := id.UnmarshalText([]byte(idString)); err != nil {
			return nil, fmt.Errorf("failed to unmarshal id line: %v", err)
		}
		ids[i] = id
	}
	return ids, nil
}

func NodeFromBase64(in string) (forest.Node, error) {
	b, err := base64.RawURLEncoding.DecodeString(in)
	if err != nil {
		return nil, fmt.Errorf("failed to decode node string: %v", err)
	}
	node, err := forest.UnmarshalBinaryNode(b)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal node from string: %v", err)
	}
	return node, nil

}
