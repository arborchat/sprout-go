package sprout_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	forest "git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
	"git.sr.ht/~whereswaldon/forest-go/testkeys"
	sprout "git.sr.ht/~whereswaldon/sprout-go"
)

type LoopbackConn struct {
	bytes.Buffer
	sync.Mutex
}

func (l *LoopbackConn) Read(b []byte) (int, error) {
	l.Lock()
	defer l.Unlock()
	return l.Buffer.Read(b)
}

func (l *LoopbackConn) Write(b []byte) (int, error) {
	l.Lock()
	defer l.Unlock()
	return l.Buffer.Write(b)
}

func (l *LoopbackConn) Close() error {
	return nil
}

func (l *LoopbackConn) LocalAddr() net.Addr {
	return &net.IPAddr{}
}

func (l *LoopbackConn) RemoteAddr() net.Addr {
	return &net.IPAddr{}
}

func (l *LoopbackConn) SetDeadline(t time.Time) error {
	if err := l.SetReadDeadline(t); err != nil {
		return err
	}
	return l.SetWriteDeadline(t)
}

func (l *LoopbackConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (l *LoopbackConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// TeeConn is a debugging tool that can be used in place of LoopbackConn
// to log all of the protocol text to an io.Writer for analysis
type TeeConn struct {
	LoopbackConn
	Out io.Writer
}

func (t *TeeConn) Write(b []byte) (int, error) {
	n, err := t.LoopbackConn.Write(b)
	if err != nil {
		return n, err
	}
	return t.Out.Write(b)
}

var _ net.Conn = &TeeConn{}

func mockConnOrFail(t *testing.T) (net.Conn, *sprout.Conn) {
	conn := new(LoopbackConn)
	sconn, err := sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	return conn, sconn
}

func TestVersionMessage(t *testing.T) {
	_, sconn := mockConnOrFail(t)
	sconn.OnVersion = func(s *sprout.Conn, m sprout.MessageID, major, minor int) error {
		if major != sconn.Major {
			t.Fatalf("major version received %d does not match sent version %d", major, sconn.Major)
		}
		if minor != sconn.Minor {
			t.Fatalf("minor version received %d does not match sent version %d", minor, sconn.Minor)
		}
		return sconn.SendStatus(m, sprout.StatusOk)
	}
	go readConnOrFail(sconn, 2, t)
	err := sconn.SendVersion(time.NewTicker(time.Second).C)
	if err != nil {
		t.Fatalf("failed to send version: %v", err)
	}
}

func TestVersionMessageAsync(t *testing.T) {
	_, sconn := mockConnOrFail(t)
	sconn.OnVersion = func(s *sprout.Conn, m sprout.MessageID, major, minor int) error {
		if sconn.Major != major {
			t.Fatalf("major version mismatch, expected %d, got %d", sconn.Major, major)
		} else if sconn.Minor != minor {
			t.Fatalf("minor version mismatch, expected %d, got %d", sconn.Minor, minor)
		}
		return s.SendStatus(m, sprout.StatusOk)
	}
	statusChan, _, err := sconn.SendVersionAsync()
	if err != nil {
		t.Fatalf("failed to send version: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyStatus(sprout.StatusOk, statusChan, t)
}

func TestListMessageAsync(t *testing.T) {
	inQuantity := 10
	inNodeType := fields.NodeTypeIdentity
	_, identities := randomNodeSlice(inQuantity, t)
	_, sconn := mockConnOrFail(t)
	sconn.OnList = func(s *sprout.Conn, m sprout.MessageID, nodeType fields.NodeType, quantity int) error {
		if quantity != inQuantity {
			t.Fatalf("requested %d nodes, but message requested %d", inQuantity, quantity)
		}
		if nodeType != inNodeType {
			t.Fatalf("requested node type %d, but message requested type %d", inNodeType, nodeType)
		}
		return s.SendResponse(m, identities)
	}
	responseChan, _, err := sconn.SendListAsync(inNodeType, inQuantity)
	if err != nil {
		t.Fatalf("failed to send query_any: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyAsyncResponse(identities, responseChan, t)
}

func TestListMessage(t *testing.T) {
	inNodeType := fields.NodeTypeIdentity
	inQuantity := 5
	_, identities := randomNodeSlice(inQuantity, t)
	_, sconn := mockConnOrFail(t)
	sconn.OnList = func(s *sprout.Conn, m sprout.MessageID, nodeType fields.NodeType, quantity int) error {
		if quantity != inQuantity {
			t.Fatalf("requested %d nodes, but message requested %d", inQuantity, quantity)
		}
		if nodeType != inNodeType {
			t.Fatalf("requested node type %d, but message requested type %d", inNodeType, nodeType)
		}
		return s.SendResponse(m, identities)
	}
	go readConnOrFail(sconn, 2, t)
	response, err := sconn.SendList(inNodeType, inQuantity, time.NewTicker(time.Second).C)
	if err != nil {
		t.Fatalf("failed to send list: %v", err)
	}
	if len(response.Nodes) != len(identities) {
		t.Fatalf("expected response of length %d, got length %d", len(identities), len(response.Nodes))
	}
	for i := range identities {
		if !identities[i].Equals(response.Nodes[i]) {
			t.Fatalf("expected nodes[%d] to be %s, not %s", i, identities[i].ID().String(), response.Nodes[i].ID().String())
		}
	}
}

func randomQualifiedHash() *fields.QualifiedHash {
	length := 32
	return &fields.QualifiedHash{
		Descriptor: fields.HashDescriptor{
			Type:   fields.HashTypeSHA512,
			Length: fields.ContentLength(length),
		},
		Blob: fields.Blob(randomBytes(length)),
	}
}

func randomQualifiedHashSlice(count int) []*fields.QualifiedHash {
	out := make([]*fields.QualifiedHash, count)
	for i := 0; i < count; i++ {
		out[i] = randomQualifiedHash()
	}
	return out
}

func randomBytes(length int) []byte {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return b
}

func randomString(length int) string {
	return string(base64.StdEncoding.EncodeToString(randomBytes(length)))
}

func randomIdentity(t *testing.T) *forest.Identity {
	signer := testkeys.Signer(t, testkeys.PrivKey1)
	name := randomString(12)
	id, err := forest.NewIdentity(signer, name, "")
	if err != nil {
		t.Errorf("Failed to generate test identity: %v", err)
		return nil
	}
	return id
}

func randomNodeSlice(length int, t *testing.T) ([]*fields.QualifiedHash, []forest.Node) {
	ids := make([]*fields.QualifiedHash, length)
	nodes := make([]forest.Node, length)
	for i := 0; i < length; i++ {
		identity := randomIdentity(t)
		ids[i] = identity.ID()
		nodes[i] = identity
	}
	return ids, nodes
}

func TestQueryMessageAsync(t *testing.T) {
	inNodeIDs, nodes := randomNodeSlice(10, t)

	_, sconn := mockConnOrFail(t)
	sconn.OnQuery = func(s *sprout.Conn, m sprout.MessageID, nodeIDs []*fields.QualifiedHash) error {
		return s.SendResponse(m, nodes)
	}
	responseChan, _, err := sconn.SendQueryAsync(inNodeIDs...)
	if err != nil {
		t.Fatalf("failed to send query: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyAsyncResponse(nodes, responseChan, t)
}

func readConnOrFail(sconn *sprout.Conn, times int, t *testing.T) {
	for i := 0; i < times; i++ {
		err := sconn.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				i--
				continue
			}
			t.Fatalf("failed to read message (iteration %d): %v", i, err)
		}
	}
}

func verifyStatus(expected sprout.StatusCode, responseChan <-chan interface{}, t *testing.T) {
	var result interface{}
	select {
	case result = <-responseChan:
	case <-time.NewTicker(10 * time.Second).C:
		t.Fatalf("Timed out waiting for status response")
	}
	status, ok := result.(sprout.Status)
	if !ok {
		t.Fatalf("expected Status on channel, got %T:", result)
	}
	if status.Code != expected {
		t.Fatalf("version status returned status %d, expected status: %d", status.Code, expected)
	}
}

func verifyAsyncResponse(nodes []forest.Node, responseChan <-chan interface{}, t *testing.T) {
	var responseGeneric interface{}
	select {
	case responseGeneric = <-responseChan:
	case <-time.NewTicker(time.Second).C:
		t.Fatalf("timed out waiting for response on channel")
	}
	response, ok := responseGeneric.(sprout.Response)
	if !ok {
		t.Fatalf("Expected to receive struct of type Response, got %T", responseGeneric)
	}
	verifyResponse(nodes, response, t)
}

func verifyResponse(nodes []forest.Node, response sprout.Response, t *testing.T) {
	if len(nodes) != len(response.Nodes) {
		t.Fatalf("node list length mismatch, expected %d, got %d", len(nodes), len(response.Nodes))
	}
	for i, n := range nodes {
		if !n.ID().Equals(response.Nodes[i].ID()) {
			t.Fatalf("node mismatch, expected %s got %s", n.ID().String(), response.Nodes[i].ID().String())
		}
	}
}

func TestQueryMessage(t *testing.T) {
	ids, identities := randomNodeSlice(10, t)

	_, sconn := mockConnOrFail(t)
	sconn.OnQuery = func(s *sprout.Conn, m sprout.MessageID, nodeIDs []*fields.QualifiedHash) error {
		return s.SendResponse(m, identities)
	}
	go readConnOrFail(sconn, 2, t)
	response, err := sconn.SendQuery(ids, time.NewTicker(time.Second).C)
	if err != nil {
		t.Fatalf("failed to send query: %v", err)
	}
	verifyResponse(identities, response, t)
}

func TestAncestryMessageAsync(t *testing.T) {
	inLevels := 5
	_, nodes := randomNodeSlice(inLevels, t)
	inNodeID := nodes[0].ID()

	_, sconn := mockConnOrFail(t)
	sconn.OnAncestry = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash, levels int) error {
		if !inNodeID.Equals(nodeID) {
			t.Fatalf("message requests ancestry for node %s, expected node %s", nodeID.String(), inNodeID.String())
		}
		if inLevels != levels {
			t.Fatalf("message requests %d levels of ancestry, expected %d levels", levels, inLevels)
		}
		return sconn.SendResponse(m, nodes)
	}
	resultChan, _, err := sconn.SendAncestryAsync(inNodeID, inLevels)
	if err != nil {
		t.Fatalf("failed to send ancestry: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyAsyncResponse(nodes, resultChan, t)
}

func TestAncestryMessage(t *testing.T) {
	ids, identities := randomNodeSlice(10, t)

	_, sconn := mockConnOrFail(t)
	sconn.OnAncestry = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash, levels int) error {
		return s.SendResponse(m, identities)
	}
	go readConnOrFail(sconn, 2, t)
	response, err := sconn.SendAncestry(ids[0], len(ids)-1, time.NewTicker(time.Second).C)
	if err != nil {
		t.Fatalf("failed to send query: %v", err)
	}
	verifyResponse(identities, response, t)
}

func TestLeavesOfMessageAsync(t *testing.T) {
	inQuantity := 5
	_, nodes := randomNodeSlice(inQuantity, t)
	inNodeID := nodes[0].ID()
	_, sconn := mockConnOrFail(t)
	sconn.OnLeavesOf = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash, quantity int) error {
		if !inNodeID.Equals(nodeID) {
			t.Fatalf("message requests leaves of node %s, but expected node %s", nodeID.String(), inNodeID.String())
		}
		if quantity != inQuantity {
			t.Fatalf("message requests %d leaves, but expected to request %d", quantity, inQuantity)
		}
		return sconn.SendResponse(m, nodes)
	}
	resultChan, _, err := sconn.SendLeavesOfAsync(inNodeID, inQuantity)
	if err != nil {
		t.Fatalf("failed to send query_any: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyAsyncResponse(nodes, resultChan, t)
}

func TestLeavesOfMessage(t *testing.T) {
	inQuantity := 5
	ids, identities := randomNodeSlice(inQuantity, t)
	_, sconn := mockConnOrFail(t)
	sconn.OnLeavesOf = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash, quantity int) error {
		return s.SendResponse(m, identities[1:])
	}
	go readConnOrFail(sconn, 2, t)
	response, err := sconn.SendLeavesOf(ids[0], inQuantity-1, time.NewTicker(time.Second).C)
	if err != nil {
		t.Fatalf("failed to send query_any: %v", err)
	}
	verifyResponse(identities[1:], response, t)
}

func TestSubscribeMessageAsync(t *testing.T) {
	inNodeID := randomQualifiedHash()

	_, sconn := mockConnOrFail(t)
	sconn.OnSubscribe = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash) error {
		if !nodeID.Equals(inNodeID) {
			t.Fatalf("expected message to subscribe to %s, got %s", inNodeID.String(), nodeID.String())
		}
		return s.SendStatus(m, sprout.StatusOk)
	}
	sconn.OnUnsubscribe = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash) error {
		if !nodeID.Equals(inNodeID) {
			t.Fatalf("expected message to subscribe to %s, got %s", inNodeID.String(), nodeID.String())
		}
		return s.SendStatus(m, sprout.StatusOk)
	}

	resultChan, _, err := sconn.SendSubscribeByIDAsync(inNodeID)
	if err != nil {
		t.Fatalf("failed to send subscribe: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyStatus(sprout.StatusOk, resultChan, t)

	resultChan2, _, err := sconn.SendUnsubscribeByIDAsync(inNodeID)
	if err != nil {
		t.Fatalf("failed to send unsubscribe: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyStatus(sprout.StatusOk, resultChan2, t)
}

func TestSubscribeMessage(t *testing.T) {
	var (
		inID, outID         sprout.MessageID
		inNodeID, outNodeID *fields.QualifiedHash
		err                 error
	)
	inNodeID = randomQualifiedHash()

	_, sconn := mockConnOrFail(t)
	sconn.OnSubscribe = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash) error {
		outID = m
		outNodeID = nodeID
		return nil
	}
	inID, err = sconn.SendSubscribeByID(inNodeID)
	if err != nil {
		t.Fatalf("failed to send subscribe: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read subscribe: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	}
	if !inNodeID.Equals(outNodeID) {
		inString, _ := inNodeID.MarshalText()
		outString, _ := outNodeID.MarshalText()
		t.Fatalf("node id mismatch, expected %s got %s", inString, outString)
	}
}

func TestUnsubscribeMessage(t *testing.T) {
	var (
		inID, outID         sprout.MessageID
		inNodeID, outNodeID *fields.QualifiedHash
		err                 error
	)
	inNodeID = randomQualifiedHash()

	_, sconn := mockConnOrFail(t)
	sconn.OnUnsubscribe = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash) error {
		outID = m
		outNodeID = nodeID
		return nil
	}
	inID, err = sconn.SendUnsubscribeByID(inNodeID)
	if err != nil {
		t.Fatalf("failed to send unsubscribe: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read unsubscribe: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	}
	if !inNodeID.Equals(outNodeID) {
		inString, _ := inNodeID.MarshalText()
		outString, _ := outNodeID.MarshalText()
		t.Fatalf("node id mismatch, expected %s got %s", inString, outString)
	}
}

func TestAnnounceMessageAsync(t *testing.T) {
	const count = 3
	_, inNodes := randomNodeSlice(count, t)
	conn := &LoopbackConn{}
	sconn, err := sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnAnnounce = func(s *sprout.Conn, m sprout.MessageID, nodes []forest.Node) error {
		if len(nodes) != len(inNodes) {
			t.Fatalf("announced %d nodes, but got announcement for %d", len(inNodes), len(nodes))
		} else {
			for i := range nodes {
				if !nodes[i].Equals(inNodes[i]) {
					t.Fatalf("expected node at position %d to be %s, not %s", i, inNodes[i].ID().String(), nodes[i].ID().String())
				}
			}
		}
		return sconn.SendStatus(m, sprout.StatusOk)
	}
	statusChan, _, err := sconn.SendAnnounceAsync(inNodes)
	if err != nil {
		t.Fatalf("failed to send response: %v", err)
	}
	go readConnOrFail(sconn, 2, t)

	verifyStatus(sprout.StatusOk, statusChan, t)
}

func TestAnnounceMessage(t *testing.T) {
	const count = 3
	_, inNodes := randomNodeSlice(count, t)
	conn := &LoopbackConn{}
	sconn, err := sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnAnnounce = func(s *sprout.Conn, m sprout.MessageID, nodes []forest.Node) error {
		return s.SendStatus(m, sprout.StatusOk)
	}
	go readConnOrFail(sconn, 2, t)
	err = sconn.SendAnnounce(inNodes, time.NewTicker(time.Second).C)
	if err != nil {
		t.Fatalf("failed to send response: %v", err)
	}
}

func TestWithRealNetConn(t *testing.T) {
	var (
		address  string
		listener net.Listener
		err      error
	)
	for i := 7000; i < 8000; i++ {
		address = fmt.Sprintf("localhost:%d", i)
		listener, err = net.Listen("tcp", address)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("Failed to create test listener: %v", err)
	}
	go func() {
		conn, err := net.Dial("tcp", address)
		if err != nil {
			t.Fatalf("Failed to dial test listener: %v", err)
		}
		defer conn.Close()
		sconn, err := sprout.NewConn(conn)
		if err != nil {
			t.Fatalf("Failed to make sprout.Conn from net.Conn: %v", err)
		}
		if err := sconn.SendVersion(nil); err != nil {
			t.Fatalf("Failed to send version: %v", err)
		}
	}()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Failed to accept test connection: %v", err)
	}
	sconn, err := sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("Failed to make sprout.Conn from net.Conn: %v", err)
	}
	out := make(chan struct {
		id           sprout.MessageID
		major, minor int
	}, 1)
	sconn.OnVersion = func(s *sprout.Conn, id sprout.MessageID, major, minor int) error {
		out <- struct {
			id           sprout.MessageID
			major, minor int
		}{id: id, major: major, minor: minor}
		return nil
	}
	if err := sconn.ReadMessage(); err != nil {
		t.Fatalf("Failed to read message from test connection: %v", err)
	}
	select {
	case <-out:
	// handler was invoked, we're all good
	case <-time.NewTicker(time.Second).C:
		t.Fatalf("Handler wasn't invoked within 1 second")
	}
}
