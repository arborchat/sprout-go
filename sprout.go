package sprout

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

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
	VersionVerb     Verb = "version"
	ListVerb        Verb = "list"
	QueryVerb       Verb = "query"
	AncestryVerb    Verb = "ancestry"
	LeavesOfVerb    Verb = "leaves_of"
	SubscribeVerb   Verb = "subscribe"
	UnsubscribeVerb Verb = "unsubscribe"
	AnnounceVerb    Verb = "announce"
	ResponseVerb    Verb = "response"
	StatusVerb      Verb = "status"
)

var formats = map[Verb]string{
	VersionVerb:     " %d %d.%d\n",
	ListVerb:        " %d %d %d\n",
	QueryVerb:       " %d %d\n",
	AncestryVerb:    " %d %s %d\n",
	LeavesOfVerb:    " %d %s %d\n",
	SubscribeVerb:   " %d %s\n",
	UnsubscribeVerb: " %d %s\n",
	AnnounceVerb:    " %d %d\n",
	ResponseVerb:    " %d %d\n",
	StatusVerb:      " %d %d\n",
}

type Status struct {
	Code StatusCode
}

func (s Status) Error() string {
	return fmt.Sprintf("%s", s.Code)
}

type Response struct {
	Nodes []forest.Node
}

type Conn struct {
	// Write side of connection, synchronized with mutex
	sync.Mutex
	Conn io.ReadWriteCloser

	// Read side of connection, buffered for parse simplicity
	BufferedConn io.Reader

	// Protocol version in use
	Major, Minor int

	nextMessageID MessageID

	// Map from messageID to channel waiting for response
	PendingStatus sync.Map

	OnVersion     func(s *Conn, messageID MessageID, major, minor int) error
	OnList        func(s *Conn, messageID MessageID, nodeType fields.NodeType, quantity int) error
	OnQuery       func(s *Conn, messageID MessageID, nodeIds []*fields.QualifiedHash) error
	OnAncestry    func(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash, levels int) error
	OnLeavesOf    func(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash, quantity int) error
	OnSubscribe   func(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash) error
	OnUnsubscribe func(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash) error
	OnAnnounce    func(s *Conn, messageID MessageID, nodes []forest.Node) error
}

// NewConn constructs a sprout connection using the provided transport. Writes to the transport
// are expected to reach the other end of the sprout connection, and reads should deliver bytes
// from the other end. The expected use is TCP connections, though other transports are possible.
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
	messageID = s.getNextMessageID()
	return s.writeMessageWithID(messageID, verb, format, fmtArgs...)
}

// writeStatusMessageAsync writes a message that expects a `status` or `response`
// message and returns the channel on which that response will be provided.
func (s *Conn) writeMessageAsync(verb Verb, format string, fmtArgs ...interface{}) (responseChan chan interface{}, id MessageID, err error) {
	messageID := s.getNextMessageID()
	responseChan = make(chan interface{})
	s.PendingStatus.Store(messageID, responseChan)
	id, err = s.writeMessageWithID(messageID, verb, format, fmtArgs...)
	return responseChan, id, err
}

func (s *Conn) getNextMessageID() MessageID {
	s.Lock()
	defer s.Unlock()
	id := s.nextMessageID
	s.nextMessageID++
	return id
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

// Cancel deallocates the response structures associated with the protocol message with the
// given identifier. This is primarily useful when the other end of the connection has not
// responded in a long time, and we are interested in cleaning up the resources used in
// waiting for them to respond. An attempt to cancel a message that is not waiting for
// a response will have no effect.
func (s *Conn) Cancel(messageID MessageID) {
	s.PendingStatus.Delete(messageID)
}

// SendVersionAsync notifies the other end of the sprout connection of our supported protocol
// version number. See the package-level documentation for details on how
// to use the Async methods properly.
func (s *Conn) SendVersionAsync() (<-chan interface{}, MessageID, error) {
	op := VersionVerb
	return s.writeMessageAsync(op, string(op)+formats[op], s.Major, s.Minor)
}

// SendVersion notifies the other end of the sprout connection of our supported protocol
// version number.
func (s *Conn) SendVersion(timeoutChan <-chan time.Time) error {
	op := VersionVerb
	statusChan, messageID, err := s.SendVersionAsync()
	return s.handleExpectedStatus(op, statusChan, messageID, err, timeoutChan)
}

func (s *Conn) handleExpectedStatus(op Verb, statusChan <-chan interface{}, messageID MessageID, err error, timeoutChan <-chan time.Time) error {
	if err != nil {
		return fmt.Errorf("failed sending %s message: %w", op, err)
	}
	select {
	case status := <-statusChan:
		asStatus, ok := status.(Status)
		if !ok {
			return fmt.Errorf("got non-status struct over response channel (type %T)", status)
		}
		if asStatus.Code != StatusOk {
			return asStatus
		}
		return nil
	case <-timeoutChan:
		s.Cancel(messageID)
		return fmt.Errorf("timed out waiting for response to %s message", op)
	}
}

// SendListAsync requests a list of recent nodes of a particular node type from the other end of
// the sprout connection. The requested quantity is the maximum number of nodes that the other
// end should provide, though it may provide significantly fewer.
// See the package level documentation for details on how to use the Async methods.
func (s *Conn) SendListAsync(nodeType fields.NodeType, quantity int) (<-chan interface{}, MessageID, error) {
	op := ListVerb
	return s.writeMessageAsync(op, string(op)+formats[op], nodeType, quantity)
}

// SendList requests a list of recent nodes of a particular node type from the other end of
// the sprout connection.
func (s *Conn) SendList(nodeType fields.NodeType, quantity int, timeoutChan <-chan time.Time) (Response, error) {
	op := ListVerb
	resultChan, messageID, err := s.SendListAsync(nodeType, quantity)
	return s.handleExpectedResponse(op, resultChan, messageID, err, timeoutChan)
}

// handleExpectedResponse waits for a response message on the resultChan it is given until it receives anything
// on timeoutChan. If a value is received on the resultChan and it is a Result, it will be returned. It it is a
// Status (indicating that something went wrong), it will be returned as an error. If the timeout occurs, the
// request will be cancelled using the provided messageID and an error will be returned.
//
// The err parameter is intended to be used as the err returned by calling one of the Async methods. This allows
// us to write the error handler only once in this function instead of once in each synchronous method.
func (s *Conn) handleExpectedResponse(op Verb, resultChan <-chan interface{}, messageID MessageID, err error, timeoutChan <-chan time.Time) (Response, error) {
	if err != nil {
		return Response{}, fmt.Errorf("failed sending %s message: %w", op, err)
	}
	select {
	case result := <-resultChan:
		asResponse, ok := result.(Response)
		if ok {
			return asResponse, nil
		}
		asStatus, ok := result.(Status)
		if ok {
			if asStatus.Code != StatusOk {
				return Response{}, asStatus
			}
			return Response{}, fmt.Errorf("peer responded with status OK but should have been Response message")
		}
		return Response{}, fmt.Errorf("received non-Status, non-Response value on response channel (type %T)", result)
	case <-timeoutChan:
		s.Cancel(messageID)
		return Response{}, fmt.Errorf("timed out waiting for response to %s message", op)
	}
}

// convert a list of node IDs into the format required by the Query message
func stringifyNodeIDs(nodeIds ...*fields.QualifiedHash) string {
	builder := &strings.Builder{}
	for _, nodeId := range nodeIds {
		b, _ := nodeId.MarshalText()
		_, _ = builder.Write(b)
		builder.WriteString("\n")
	}
	return builder.String()
}

// SendQueryAsync requests the nodes with a list of IDs from the other side of the
// sprout connection. See the package level documentation for details on how to
// use the Async methods.
func (s *Conn) SendQueryAsync(nodeIds ...*fields.QualifiedHash) (<-chan interface{}, MessageID, error) {
	op := QueryVerb
	return s.writeMessageAsync(op, string(op)+formats[op]+"%s", len(nodeIds), stringifyNodeIDs(nodeIds...))
}

// SendQuery requests the nodes with a list of IDs from the other side of the
// sprout connection.
func (s *Conn) SendQuery(nodeIds []*fields.QualifiedHash, timeoutChan <-chan time.Time) (Response, error) {
	op := QueryVerb
	resultChan, messageID, err := s.SendQueryAsync(nodeIds...)
	return s.handleExpectedResponse(op, resultChan, messageID, err, timeoutChan)
}

// SendAncestry requests the ancestry of the node with the given id. The levels
// parameter specifies the maximum number of leves of ancestry to return.
// See the package-level documentation for details on how to use the Async
// methods.
func (s *Conn) SendAncestryAsync(nodeID *fields.QualifiedHash, levels int) (<-chan interface{}, MessageID, error) {
	op := AncestryVerb
	return s.writeMessageAsync(op, string(op)+formats[op], nodeID.String(), levels)
}

// SendAncestry requests the ancestry of the node with the given id. The levels
// parameter specifies the maximum number of leves of ancestry to return.
func (s *Conn) SendAncestry(nodeID *fields.QualifiedHash, levels int, timeoutChan <-chan time.Time) (Response, error) {
	op := AncestryVerb
	resultChan, messageID, err := s.SendAncestryAsync(nodeID, levels)
	return s.handleExpectedResponse(op, resultChan, messageID, err, timeoutChan)
}

// SendLeavesOf returns up to quantity nodes that are leaves in the tree rooted
// at the given ID. For a description of how to use the Async methods, see the package-level documentation.
func (s *Conn) SendLeavesOfAsync(nodeId *fields.QualifiedHash, quantity int) (<-chan interface{}, MessageID, error) {
	op := LeavesOfVerb
	return s.writeMessageAsync(op, string(op)+formats[op], nodeId.String(), quantity)
}

// SendLeavesOf returns up to quantity nodes that are leaves in the tree rooted
// at the given ID.
func (s *Conn) SendLeavesOf(nodeId *fields.QualifiedHash, quantity int, timeoutChan <-chan time.Time) (Response, error) {
	op := LeavesOfVerb
	resultChan, messageID, err := s.SendLeavesOfAsync(nodeId, quantity)
	return s.handleExpectedResponse(op, resultChan, messageID, err, timeoutChan)
}

const nodeLineFormat = "%s %s\n"

func nodeLine(n forest.Node) string {
	id, _ := n.ID().MarshalText()
	data, _ := n.MarshalBinary()
	return fmt.Sprintf(nodeLineFormat, string(id), base64.RawURLEncoding.EncodeToString(data))
}

func (s *Conn) SendResponse(msgID MessageID, nodes []forest.Node) error {
	builder := &strings.Builder{}
	for _, n := range nodes {
		builder.WriteString(nodeLine(n))
	}
	op := ResponseVerb
	_, err := s.writeMessageWithID(msgID, op, string(op)+formats[op]+"%s", len(nodes), builder.String())
	return err
}

func (s *Conn) subscribeOp(op Verb, community *forest.Community, timeoutChan <-chan time.Time) error {
	return s.subscribeOpID(op, community.ID(), timeoutChan)
}

func (s *Conn) subscribeOpAsync(op Verb, community *forest.Community) (<-chan interface{}, MessageID, error) {
	return s.subscribeOpIDAsync(op, community.ID())
}

func (s *Conn) subscribeOpID(op Verb, community *fields.QualifiedHash, timeoutChan <-chan time.Time) error {
	statusChan, messageID, err := s.subscribeOpIDAsync(op, community)
	return s.handleExpectedStatus(op, statusChan, messageID, err, timeoutChan)
}

func (s *Conn) subscribeOpIDAsync(op Verb, community *fields.QualifiedHash) (<-chan interface{}, MessageID, error) {
	return s.writeMessageAsync(op, string(op)+formats[op], community.String())
}

// SendSubscribeAsync attempts to add the given community ID to the list of subscribed
// IDs for this connection. If it succeeds, both peers are required to exchange
// new nodes for that community using Announce(). For details on how to use
// Async methods, see the package-level documentation.
func (s *Conn) SendSubscribeAsync(community *forest.Community) (<-chan interface{}, MessageID, error) {
	return s.subscribeOpAsync(SubscribeVerb, community)
}

// SendUnsubscribeAsync attempts to add the given community ID to the list of subscribed
// IDs for this connection. If it succeeds, both peers are required to exchange
// new nodes for that community using Announce(). For details on how to use
// Async methods, see the package-level documentation.
func (s *Conn) SendUnsubscribeAsync(community *forest.Community) (<-chan interface{}, MessageID, error) {
	return s.subscribeOpAsync(UnsubscribeVerb, community)
}

// SendSubscribeByIDAsync attempts to add the given community ID to the list of subscribed
// IDs for this connection. If it succeeds, both peers are required to exchange
// new nodes for that community using Announce(). For details on how to use
// Async methods, see the package-level documentation.
func (s *Conn) SendSubscribeByIDAsync(community *fields.QualifiedHash) (<-chan interface{}, MessageID, error) {
	return s.subscribeOpIDAsync(SubscribeVerb, community)
}

// SendUnsubscribeByIDAsync attempts to add the given community ID to the list of subscribed
// IDs for this connection. If it succeeds, both peers are required to exchange
// new nodes for that community using Announce(). For details on how to use
// Async methods, see the package-level documentation.
func (s *Conn) SendUnsubscribeByIDAsync(community *fields.QualifiedHash) (<-chan interface{}, MessageID, error) {
	return s.subscribeOpIDAsync(UnsubscribeVerb, community)
}

// SendSubscribe attempts to add the given community ID to the list of subscribed
// IDs for this connection. If it succeeds, both peers are required to exchange
// new nodes for that community using Announce().
func (s *Conn) SendSubscribe(community *forest.Community, timeoutChan <-chan time.Time) error {
	return s.subscribeOp(SubscribeVerb, community, timeoutChan)
}

// SendUnsubscribe attempts to add the given community ID to the list of subscribed
// IDs for this connection. If it succeeds, both peers are required to exchange
// new nodes for that community using Announce().
func (s *Conn) SendUnsubscribe(community *forest.Community, timeoutChan <-chan time.Time) error {
	return s.subscribeOp(UnsubscribeVerb, community, timeoutChan)
}

// SendSubscribeByID attempts to add the given community ID to the list of subscribed
// IDs for this connection. If it succeeds, both peers are required to exchange
// new nodes for that community using Announce().
func (s *Conn) SendSubscribeByID(community *fields.QualifiedHash, timeoutChan <-chan time.Time) error {
	return s.subscribeOpID(SubscribeVerb, community, timeoutChan)
}

// SendUnsubscribeByID attempts to add the given community ID to the list of subscribed
// IDs for this connection. If it succeeds, both peers are required to exchange
// new nodes for that community using Announce().
func (s *Conn) SendUnsubscribeByID(community *fields.QualifiedHash, timeoutChan <-chan time.Time) error {
	return s.subscribeOpID(UnsubscribeVerb, community, timeoutChan)
}

// StatusCode represents the status of a sprout protocol message.
type StatusCode int

const (
	StatusOk            StatusCode = 0
	ErrorMalformed      StatusCode = 1
	ErrorProtocolTooOld StatusCode = 2
	ErrorProtocolTooNew StatusCode = 3
	ErrorUnknownNode    StatusCode = 4
)

// String converts the status code into a human-readable error message
func (s StatusCode) String() string {
	description := ""
	switch s {
	case StatusOk:
		description = "ok"
	case ErrorMalformed:
		description = "malformed protocol message"
	case ErrorProtocolTooOld:
		description = "protocol too old"
	case ErrorProtocolTooNew:
		description = "protocol too new"
	case ErrorUnknownNode:
		description = "referenced unknown node"
	}
	return fmt.Sprintf("status code %d (%s)", s, description)
}

// SendStatus responds to the message with the give targetMessageID with the
// given status code. It is always synchronous, and will return any error
// in transmitting the message.
func (s *Conn) SendStatus(targetMessageID MessageID, errorCode StatusCode) error {
	op := StatusVerb
	_, err := s.writeMessageWithID(targetMessageID, op, string(op)+formats[op], errorCode)
	return err
}

func stringifyNodes(nodes []forest.Node) string {
	builder := &strings.Builder{}
	for _, node := range nodes {
		builder.WriteString(nodeLine(node))
	}
	return builder.String()
}

// SendAnnounceAsync announces the existence of the given nodes to the peer
// on the other end of the sprout connection. See the package-level documentation
// for details on how to use Async methods.
func (s *Conn) SendAnnounceAsync(nodes []forest.Node) (<-chan interface{}, MessageID, error) {
	op := AnnounceVerb
	return s.writeMessageAsync(op, string(op)+formats[op]+"%s", len(nodes), stringifyNodes(nodes))
}

// SendAnnounce announces the existence of the given nodes to the peer
// on the other end of the sprout connection.
func (s *Conn) SendAnnounce(nodes []forest.Node, timeoutChan <-chan time.Time) error {
	op := AnnounceVerb
	responseChan, messageID, err := s.SendAnnounceAsync(nodes)
	return s.handleExpectedStatus(op, responseChan, messageID, err, timeoutChan)
}

// scanOp scans the fields for the given verb from the input connection and into
// the provided fields slice.
func (s *Conn) scanOp(verb Verb, fields ...interface{}) error {
	n, err := fmt.Fscanf(s.BufferedConn, formats[verb], fields...)
	if err != nil {
		return fmt.Errorf("failed to scan %s: %v", verb, err)
	} else if n < len(fields) {
		return fmt.Errorf("failed to scan enough arguments for %s (got %d, expected %d)", verb, n, len(fields))
	}
	return nil
}

// UnsolicitedMessageError is an error indicating that a sprout peer sent a
// response or status message with an ID that was unexpected. This could
// occur when we cancelled waiting on a request (such as a timeout), when the
// peer has a bug (double response, incorrect target message id in response),
// or when the peer is misbehaving.
type UnsolicitedMessageError struct {
	// The ID that the unsolicited message was in response to
	MessageID
}

func (u UnsolicitedMessageError) Error() string {
	return fmt.Sprintf("received status or response message for id %d, but nothing was waiting for that id", u.MessageID)
}

// sendToWaitingChannel looks up the channel associated with a given messageID
// and sends the given data on that channel.
func (s *Conn) sendToWaitingChannel(data interface{}, messageID MessageID) error {
	waitingChan, ok := s.PendingStatus.Load(messageID)
	if !ok {
		// discard if nothing is waiting.
		return UnsolicitedMessageError{MessageID: messageID}
	}
	s.PendingStatus.Delete(messageID)
	statusChan, ok := waitingChan.(chan interface{})
	if !ok {
		return fmt.Errorf("found item in map for message id %d, but isn't type chan interface{}, is %T", messageID, waitingChan)
	}
	statusChan <- data
	close(statusChan)
	return nil
}

// ReadMessage reads and parses a single sprout protocol message off of the
// connection. It calls the appropriate OnVerb handler function when it
// parses a message, and it returns any parse errors. It will block when no messages are available.
//
// This method must be called in a loop in order for the sprout connection
// to be able to receive messages properly. This isn't done automatically
// by the Conn type in order to provide flexibility on how to handler errors
// from this method. The Worker type can wrap a Conn to both implement its
// handler functions and call this method automatically.
//
// This method may return an UnsolicitedMessageError in some cases. This may
// be due to a local timeout/request cancellation, and should generally not
// be cause to close the connection entirely.
func (s *Conn) ReadMessage() error {
	var word string
	n, err := fmt.Fscanf(s.BufferedConn, "%s", &word)
	if err != nil {
		return fmt.Errorf("error scanning verb: %w", err)
	} else if n < 1 {
		return fmt.Errorf("failed to read a verb")
	}
	verb := Verb(word)
	switch verb {
	case VersionVerb:
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
			return fmt.Errorf("error running hook for %s: %w", verb, err)
		}
	case ListVerb:
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
			return fmt.Errorf("error running hook for %s: %w", verb, err)
		}
	case QueryVerb:
		var (
			messageID MessageID
			count     int
		)
		if err := s.scanOp(verb, &messageID, &count); err != nil {
			return err
		}
		ids, err := s.readNodeIDs(count)
		if err != nil {
			return fmt.Errorf("failed to read node ids in query message: %w", err)
		}
		if s.OnQuery == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnQuery(s, messageID, ids); err != nil {
			return fmt.Errorf("error running hook for %s: %w", verb, err)
		}
	case AncestryVerb:
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
			return fmt.Errorf("failed to unmarshal ancestry target: %w", err)
		}

		if s.OnAncestry == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnAncestry(s, messageID, id, levels); err != nil {
			return fmt.Errorf("error running hook for %s: %w", verb, err)
		}
	case LeavesOfVerb:
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
			return fmt.Errorf("failed to unmarshal leaves_of target: %w", err)
		}
		if s.OnLeavesOf == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnLeavesOf(s, messageID, id, quantity); err != nil {
			return fmt.Errorf("error running hook for %s: %w", verb, err)
		}
	case ResponseVerb:
		var (
			targetMessageID MessageID
			count           int
			response        Response
			err             error
		)
		if err := s.scanOp(verb, &targetMessageID, &count); err != nil {
			return err
		}
		response.Nodes, err = s.readNodeLines(count)
		if err != nil {
			return fmt.Errorf("failed reading response node list: %w", err)
		}
		if err := s.sendToWaitingChannel(response, targetMessageID); err != nil {
			return fmt.Errorf("failed sending response to waiting channel: %w", err)
		}
	case SubscribeVerb:
		fallthrough
	case UnsubscribeVerb:
		var (
			messageID    MessageID
			nodeIDString string
		)
		if err := s.scanOp(verb, &messageID, &nodeIDString); err != nil {
			return err
		}
		id := &fields.QualifiedHash{}
		if err := id.UnmarshalText([]byte(nodeIDString)); err != nil {
			return fmt.Errorf("failed to unmarshal %s target: %w", verb, err)
		}

		hook := s.OnSubscribe
		if verb == UnsubscribeVerb {
			hook = s.OnUnsubscribe
		}
		if hook == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := hook(s, messageID, id); err != nil {
			return fmt.Errorf("error running hook for %s: %w", verb, err)
		}
	case StatusVerb:
		var (
			status    Status
			messageID MessageID
		)
		if err := s.scanOp(verb, &messageID, &status.Code); err != nil {
			return fmt.Errorf("failed scanning status message: %w", err)
		}
		if err := s.sendToWaitingChannel(status, messageID); err != nil {
			return fmt.Errorf("failed sending status to waiting channel: %w", err)
		}
	case AnnounceVerb:
		var (
			messageID MessageID
			count     int
		)
		if err := s.scanOp(verb, &messageID, &count); err != nil {
			return err
		}
		nodes, err := s.readNodeLines(count)
		if err != nil {
			return fmt.Errorf("failed parsing announce node list: %w", err)
		}

		if s.OnAnnounce == nil {
			return fmt.Errorf("no handler set for verb %s", verb)
		}
		if err := s.OnAnnounce(s, messageID, nodes); err != nil {
			return fmt.Errorf("error running hook for %s: %w", verb, err)
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
			return nil, fmt.Errorf("failed to unmarshal node id %s: %v", idString, err)
		}
		node, err := NodeFromBase64URL(nodeString)
		if err != nil {
			return nil, fmt.Errorf("failed to read node %s: %v", nodeString, err)
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

func NodeFromBase64URL(in string) (forest.Node, error) {
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
