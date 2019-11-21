package sprout

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"

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
	List        Verb = "list"
	Query       Verb = "query"
	Ancestry    Verb = "ancestry"
	LeavesOf    Verb = "leaves_of"
	Subscribe   Verb = "subscribe"
	Unsubscribe Verb = "unsubscribe"
	Announce    Verb = "announce"
	Response    Verb = "response"
	Status      Verb = "status"
)

var formats = map[Verb]string{
	Version:     " %d %d.%d\n",
	List:        " %d %d %d\n",
	Query:       " %d %d\n",
	Ancestry:    " %d %s %d\n",
	LeavesOf:    " %d %s %d\n",
	Subscribe:   " %d %s\n",
	Unsubscribe: " %d %s\n",
	Announce:    " %d %d\n",
	Response:    " %d %d\n",
	Status:      " %d %d\n",
}

type Conn struct {
	Conn          io.ReadWriteCloser
	BufferedConn  io.Reader
	Major, Minor  int
	nextMessageID MessageID
	sync.Mutex

	OnVersion     func(s *Conn, messageID MessageID, major, minor int) error
	OnList        func(s *Conn, messageID MessageID, nodeType fields.NodeType, quantity int) error
	OnQuery       func(s *Conn, messageID MessageID, nodeIds []*fields.QualifiedHash) error
	OnAncestry    func(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash, levels int) error
	OnLeavesOf    func(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash, quantity int) error
	OnResponse    func(s *Conn, targetMessageID MessageID, nodes []forest.Node) error
	OnSubscribe   func(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash) error
	OnUnsubscribe func(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash) error
	OnStatus      func(s *Conn, messageID MessageID, code StatusCode) error
	OnAnnounce    func(s *Conn, messageID MessageID, nodes []forest.Node) error
}

func NewConn(transport io.ReadWriteCloser) (*Conn, error) {
	type bufferedConn struct {
		io.Reader
		io.WriteCloser
	}
	s := &Conn{
		Major:         CurrentMajor,
		Minor:         CurrentMinor,
		nextMessageID: 0,
		// Reader must be buffered so that Fscanf can Unread characters
		BufferedConn: bufio.NewReader(transport),
		Conn:         transport,
	}
	return s, nil
}

func (s *Conn) writeMessage(verb Verb, format string, fmtArgs ...interface{}) (messageID MessageID, err error) {
	messageID = s.nextMessageID
	s.nextMessageID++
	return s.writeMessageWithID(messageID, verb, format, fmtArgs...)
}

func (s *Conn) writeMessageWithID(messageIDIn MessageID, verb Verb, format string, fmtArgs ...interface{}) (messageID MessageID, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to send %s: %v", string(verb), err)
		}
	}()
	opts := make([]interface{}, 1, len(fmtArgs)+1)
	opts[0] = messageIDIn
	opts = append(opts, fmtArgs...)
	messageID = messageIDIn
	s.Lock()
	defer s.Unlock()
	_, err = fmt.Fprintf(s.Conn, format, opts...)
	return messageID, err
}

func (s *Conn) SendVersion() (MessageID, error) {
	op := Version
	return s.writeMessage(op, string(op)+formats[op], s.Major, s.Minor)
}

func (s *Conn) SendList(nodeType fields.NodeType, quantity int) (MessageID, error) {
	op := List
	return s.writeMessage(op, string(op)+formats[op], nodeType, quantity)
}

func (s *Conn) SendQuery(nodeIds ...*fields.QualifiedHash) (MessageID, error) {
	builder := &strings.Builder{}
	for _, nodeId := range nodeIds {
		b, _ := nodeId.MarshalText()
		_, _ = builder.Write(b)
		builder.WriteString("\n")
	}
	op := Query
	return s.writeMessage(op, string(op)+formats[op]+"%s", len(nodeIds), builder.String())
}

func (s *Conn) SendAncestry(nodeID *fields.QualifiedHash, levels int) (MessageID, error) {
	id, _ := nodeID.MarshalText()
	op := Ancestry
	return s.writeMessage(op, string(op)+formats[op], id, levels)
}

func (s *Conn) SendLeavesOf(nodeId *fields.QualifiedHash, quantity int) (MessageID, error) {
	id, _ := nodeId.MarshalText()
	op := LeavesOf
	return s.writeMessage(op, string(op)+formats[op], string(id), quantity)
}

const nodeLineFormat = "%s %s\n"

func NodeLine(n forest.Node) string {
	id, _ := n.ID().MarshalText()
	data, _ := n.MarshalBinary()
	return fmt.Sprintf(nodeLineFormat, string(id), base64.RawURLEncoding.EncodeToString(data))
}

func (s *Conn) SendResponse(msgID MessageID, nodes []forest.Node) error {
	builder := &strings.Builder{}
	for _, n := range nodes {
		builder.WriteString(NodeLine(n))
	}
	op := Response
	_, err := s.writeMessageWithID(msgID, op, string(op)+formats[op]+"%s", len(nodes), builder.String())
	return err
}

func (s *Conn) subscribeOp(op Verb, community *forest.Community) (MessageID, error) {
	return s.subscribeOpID(op, community.ID())
}

func (s *Conn) subscribeOpID(op Verb, community *fields.QualifiedHash) (MessageID, error) {
	id, _ := community.MarshalText()
	return s.writeMessage(op, string(op)+formats[op], id)
}

func (s *Conn) SendSubscribe(community *forest.Community) (MessageID, error) {
	return s.subscribeOp(Subscribe, community)
}

func (s *Conn) SendUnsubscribe(community *forest.Community) (MessageID, error) {
	return s.subscribeOp(Unsubscribe, community)
}

func (s *Conn) SendSubscribeByID(community *fields.QualifiedHash) (MessageID, error) {
	return s.subscribeOpID(Subscribe, community)
}

func (s *Conn) SendUnsubscribeByID(community *fields.QualifiedHash) (MessageID, error) {
	return s.subscribeOpID(Unsubscribe, community)
}

type StatusCode int

const (
	StatusOk            StatusCode = 0
	ErrorMalformed      StatusCode = 1
	ErrorProtocolTooOld StatusCode = 2
	ErrorProtocolTooNew StatusCode = 3
	ErrorUnknownNode    StatusCode = 4
)

func (s *Conn) SendStatus(targetMessageID MessageID, errorCode StatusCode) error {
	op := Status
	_, err := s.writeMessageWithID(targetMessageID, op, string(op)+formats[op], errorCode)
	return err
}

func (s *Conn) SendAnnounce(nodes []forest.Node) (messageID MessageID, err error) {
	builder := &strings.Builder{}
	for _, node := range nodes {
		builder.WriteString(NodeLine(node))
	}
	op := Announce

	return s.writeMessage(op, string(op)+formats[op]+"%s", len(nodes), builder.String())
}

func (s *Conn) scanOp(verb Verb, fields ...interface{}) error {
	n, err := fmt.Fscanf(s.BufferedConn, formats[verb], fields...)
	if err != nil {
		return fmt.Errorf("failed to scan %s: %v", verb, err)
	} else if n < len(fields) {
		return fmt.Errorf("failed to scan enough arguments for %s (got %d, expected %d)", verb, n, len(fields))
	}
	return nil
}

func (s *Conn) ReadMessage() error {
	var word string
	n, err := fmt.Fscanf(s.BufferedConn, "%s", &word)
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
		if s.OnVersion == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnVersion(s, messageID, major, minor); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case List:
		var (
			messageID MessageID
			nodeType  fields.NodeType
			quantity  int
		)
		if err := s.scanOp(verb, &messageID, &nodeType, &quantity); err != nil {
			return err
		}
		if s.OnList == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnList(s, messageID, nodeType, quantity); err != nil {
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
		if s.OnQuery == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnQuery(s, messageID, ids); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case Ancestry:
		var (
			messageID    MessageID
			nodeIDString string
			levels       int
		)
		if err := s.scanOp(verb, &messageID, &nodeIDString, &levels); err != nil {
			return err
		}
		id := &fields.QualifiedHash{}
		if err := id.UnmarshalText([]byte(nodeIDString)); err != nil {
			return fmt.Errorf("failed to unmarshal ancestry target: %v", err)
		}

		if s.OnAncestry == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnAncestry(s, messageID, id, levels); err != nil {
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
			return fmt.Errorf("failed to unmarshal leaves_of target: %v", err)
		}
		if s.OnLeavesOf == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnLeavesOf(s, messageID, id, quantity); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case Response:
		var (
			targetMessageID MessageID
			count           int
		)
		if err := s.scanOp(verb, &targetMessageID, &count); err != nil {
			return err
		}
		nodes, err := s.readNodeLines(count)
		if err != nil {
			return fmt.Errorf("failed reading response node list: %v", err)
		}
		if s.OnResponse == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnResponse(s, targetMessageID, nodes); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case Subscribe:
		fallthrough
	case Unsubscribe:
		var (
			messageID    MessageID
			nodeIDString string
		)
		if err := s.scanOp(verb, &messageID, &nodeIDString); err != nil {
			return err
		}
		id := &fields.QualifiedHash{}
		if err := id.UnmarshalText([]byte(nodeIDString)); err != nil {
			return fmt.Errorf("failed to unmarshal %s target: %v", verb, err)
		}

		hook := s.OnSubscribe
		if verb == Unsubscribe {
			hook = s.OnUnsubscribe
		}
		if hook == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := hook(s, messageID, id); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case Status:
		var (
			errorCode StatusCode
			messageID MessageID
		)
		if err := s.scanOp(verb, &messageID, &errorCode); err != nil {
			return err
		}
		if s.OnStatus == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnStatus(s, messageID, errorCode); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	case Announce:
		var (
			messageID MessageID
			count     int
		)
		if err := s.scanOp(verb, &messageID, &count); err != nil {
			return err
		}
		nodes, err := s.readNodeLines(count)
		if err != nil {
			return fmt.Errorf("failed parsing announce node list: %v", err)
		}

		if s.OnAnnounce == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnAnnounce(s, messageID, nodes); err != nil {
			return fmt.Errorf("error running hook for %s: %v", verb, err)
		}
	}
	return nil
}

func (s *Conn) readNodeLines(count int) ([]forest.Node, error) {
	nodes := make([]forest.Node, count)
	for i := 0; i < count; i++ {
		var (
			idString   string
			nodeString string
		)
		n, err := fmt.Fscanf(s.BufferedConn, nodeLineFormat, &idString, &nodeString)
		if err != nil {
			return nil, fmt.Errorf("error reading node line: %v", err)
		} else if n != 2 {
			return nil, fmt.Errorf("unexpected number of items, expected %d found %d", 2, n)
		}
		id := &fields.QualifiedHash{}
		if err := id.UnmarshalText([]byte(idString)); err != nil {
			return nil, fmt.Errorf("failed to unmarshal node line: %v", err)
		}
		node, err := NodeFromBase64(nodeString)
		if err != nil {
			return nil, fmt.Errorf("failed to read node: %v", err)
		}
		if !node.ID().Equals(id) {
			expectedIDString, _ := id.MarshalText()
			actualIDString, _ := node.ID().MarshalText()
			return nil, fmt.Errorf("message id mismatch, node given as %s hashes to %s", expectedIDString, actualIDString)
		}
		nodes[i] = node
	}
	return nodes, nil
}

func (s *Conn) readNodeIDs(count int) ([]*fields.QualifiedHash, error) {
	ids := make([]*fields.QualifiedHash, count)
	for i := 0; i < count; i++ {
		var idString string
		n, err := fmt.Fscanln(s.BufferedConn, &idString)
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
