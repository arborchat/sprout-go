package sprout_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand"
	"net"
	"testing"
	"time"

	forest "git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
	"git.sr.ht/~whereswaldon/forest-go/testkeys"
	sprout "git.sr.ht/~whereswaldon/sprout-go"
)

type LoopbackConn struct {
	bytes.Buffer
}

func (l LoopbackConn) Close() error {
	return nil
}

func (l LoopbackConn) LocalAddr() net.Addr {
	return &net.IPAddr{}
}

func (l LoopbackConn) RemoteAddr() net.Addr {
	return &net.IPAddr{}
}

func (l LoopbackConn) SetDeadline(t time.Time) error {
	if err := l.SetReadDeadline(t); err != nil {
		return err
	}
	return l.SetWriteDeadline(t)
}
func (l LoopbackConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (l LoopbackConn) SetWriteDeadline(t time.Time) error {
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

func TestVersionMessage(t *testing.T) {
	var (
		outMajor    int
		outMinor    int
		inID, outID sprout.MessageID
		err         error
		sconn       *sprout.Conn
	)
	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnVersion = func(s *sprout.Conn, m sprout.MessageID, major, minor int) error {
		outID = m
		outMajor = major
		outMinor = minor
		return nil
	}
	inID, err = sconn.SendVersion()
	if err != nil {
		t.Fatalf("failed to send version: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to send version: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	} else if sconn.Major != outMajor {
		t.Fatalf("major version mismatch, expected %d, got %d", sconn.Major, outMajor)
	} else if sconn.Minor != outMinor {
		t.Fatalf("minor version mismatch, expected %d, got %d", sconn.Minor, outMinor)
	}
}

func TestVersionMessageAsync(t *testing.T) {
	conn := new(LoopbackConn)
	sconn, err := sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnVersion = func(s *sprout.Conn, m sprout.MessageID, major, minor int) error {
		if sconn.Major != major {
			t.Fatalf("major version mismatch, expected %d, got %d", sconn.Major, major)
		} else if sconn.Minor != minor {
			t.Fatalf("minor version mismatch, expected %d, got %d", sconn.Minor, minor)
		}
		return s.SendStatus(m, sprout.StatusOk)
	}
	statusChan, err := sconn.SendVersionAsync()
	if err != nil {
		t.Fatalf("failed to send version: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyStatus(sprout.StatusOk, statusChan, t)
}

func TestListMessageAsync(t *testing.T) {
	var (
		err   error
		sconn *sprout.Conn
	)
	inQuantity := 10
	inNodeType := fields.NodeTypeIdentity
	_, identities := randomNodeSlice(inQuantity, t)
	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnList = func(s *sprout.Conn, m sprout.MessageID, nodeType fields.NodeType, quantity int) error {
		if quantity != inQuantity {
			t.Fatalf("requested %d nodes, but message requested %d", inQuantity, quantity)
		}
		if nodeType != inNodeType {
			t.Fatalf("requested node type %d, but message requested type %d", inNodeType, nodeType)
		}
		return s.SendResponse(m, identities)
	}
	responseChan, err := sconn.SendListAsync(inNodeType, inQuantity)
	if err != nil {
		t.Fatalf("failed to send query_any: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyResponse(identities, responseChan, t)
}

func TestListMessage(t *testing.T) {
	var (
		inID, outID             sprout.MessageID
		inNodeType, outNodeType fields.NodeType
		inQuantity, outQuantity int
		err                     error
		sconn                   *sprout.Conn
	)
	inNodeType = fields.NodeTypeCommunity
	inQuantity = 5
	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnList = func(s *sprout.Conn, m sprout.MessageID, nodeType fields.NodeType, quantity int) error {
		outID = m
		outNodeType = nodeType
		outQuantity = quantity
		return nil
	}
	inID, err = sconn.SendList(inNodeType, inQuantity)
	if err != nil {
		t.Fatalf("failed to send query_any: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read query_any: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	} else if inNodeType != outNodeType {
		t.Fatalf("node type mismatch, expected %d, got %d", inNodeType, outNodeType)
	} else if inQuantity != outQuantity {
		t.Fatalf("quantity mismatch, expected %d, got %d", inQuantity, outQuantity)
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
	var (
		err   error
		sconn *sprout.Conn
	)
	inNodeIDs, nodes := randomNodeSlice(10, t)

	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnQuery = func(s *sprout.Conn, m sprout.MessageID, nodeIDs []*fields.QualifiedHash) error {
		return s.SendResponse(m, nodes)
	}
	responseChan, err := sconn.SendQueryAsync(inNodeIDs...)
	if err != nil {
		t.Fatalf("failed to send query: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyResponse(nodes, responseChan, t)
}

func readConnOrFail(sconn *sprout.Conn, times int, t *testing.T) {
	for i := 0; i < times; i++ {
		err := sconn.ReadMessage()
		if err != nil {
			t.Fatalf("failed to read message: %v", err)
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

func verifyResponse(nodes []forest.Node, responseChan <-chan interface{}, t *testing.T) {
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
	var (
		inID, outID           sprout.MessageID
		inNodeIDs, outNodeIDs []*fields.QualifiedHash
		err                   error
		sconn                 *sprout.Conn
	)
	inNodeIDs = randomQualifiedHashSlice(10)

	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnQuery = func(s *sprout.Conn, m sprout.MessageID, nodeIDs []*fields.QualifiedHash) error {
		outID = m
		outNodeIDs = nodeIDs
		return nil
	}
	inID, err = sconn.SendQuery(inNodeIDs...)
	if err != nil {
		t.Fatalf("failed to send query: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read query: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	} else if len(inNodeIDs) != len(outNodeIDs) {
		t.Fatalf("node id list length mismatch, expected %d, got %d", len(inNodeIDs), len(outNodeIDs))
	}
	for i, n := range inNodeIDs {
		if !n.Equals(outNodeIDs[i]) {
			inString, _ := n.MarshalText()
			outString, _ := outNodeIDs[i].MarshalText()
			t.Fatalf("node id mismatch, expected %s got %s", inString, outString)
		}
	}
}
func TestAncestryMessageAsync(t *testing.T) {
	var (
		err   error
		sconn *sprout.Conn
	)
	inLevels := 5
	_, nodes := randomNodeSlice(inLevels, t)
	inNodeID := nodes[0].ID()

	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnAncestry = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash, levels int) error {
		if !inNodeID.Equals(nodeID) {
			t.Fatalf("message requests ancestry for node %s, expected node %s", nodeID.String(), inNodeID.String())
		}
		if inLevels != levels {
			t.Fatalf("message requests %d levels of ancestry, expected %d levels", levels, inLevels)
		}
		return sconn.SendResponse(m, nodes)
	}
	resultChan, err := sconn.SendAncestryAsync(inNodeID, inLevels)
	if err != nil {
		t.Fatalf("failed to send ancestry: %v", err)
	}
	go readConnOrFail(sconn, 2, t)
	verifyResponse(nodes, resultChan, t)
}

func TestAncestryMessage(t *testing.T) {
	var (
		inID, outID         sprout.MessageID
		inNodeID, outNodeID *fields.QualifiedHash
		inLevels, outLevels int
		err                 error
		sconn               *sprout.Conn
	)
	inNodeID = randomQualifiedHash()
	inLevels = 5

	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnAncestry = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash, levels int) error {
		outID = m
		outNodeID = nodeID
		outLevels = levels
		return nil
	}
	inID, err = sconn.SendAncestry(inNodeID, inLevels)
	if err != nil {
		t.Fatalf("failed to send ancestry: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read ancestry: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	} else if inLevels != outLevels {
		t.Fatalf("levels mismatch, expected %d, got %d", inLevels, outLevels)
	}
	if !inNodeID.Equals(outNodeID) {
		inString, _ := inNodeID.MarshalText()
		outString, _ := outNodeID.MarshalText()
		t.Fatalf("node id mismatch, expected %s got %s", inString, outString)
	}
}

func TestLeavesOfMessage(t *testing.T) {
	var (
		inID, outID             sprout.MessageID
		inNodeID, outNodeID     *fields.QualifiedHash
		inQuantity, outQuantity int
		err                     error
		sconn                   *sprout.Conn
	)
	inNodeID = randomQualifiedHash()
	inQuantity = 5
	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnLeavesOf = func(s *sprout.Conn, m sprout.MessageID, nodeID *fields.QualifiedHash, quantity int) error {
		outID = m
		outNodeID = nodeID
		outQuantity = quantity
		return nil
	}
	inID, err = sconn.SendLeavesOf(inNodeID, inQuantity)
	if err != nil {
		t.Fatalf("failed to send query_any: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read query_any: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	} else if !inNodeID.Equals(outNodeID) {
		inString, _ := inNodeID.MarshalText()
		outString, _ := outNodeID.MarshalText()
		t.Fatalf("node id mismatch, expected %s, got %s", inString, outString)
	} else if inQuantity != outQuantity {
		t.Fatalf("quantity mismatch, expected %d, got %d", inQuantity, outQuantity)
	}
}

func TestSubscribeMessage(t *testing.T) {
	var (
		inID, outID         sprout.MessageID
		inNodeID, outNodeID *fields.QualifiedHash
		err                 error
		sconn               *sprout.Conn
	)
	inNodeID = randomQualifiedHash()

	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
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
		sconn               *sprout.Conn
	)
	inNodeID = randomQualifiedHash()

	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
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

func TestAnnounceMessage(t *testing.T) {
	var (
		inID, outID       sprout.MessageID
		inNodes, outNodes []forest.Node
		err               error
		sconn             *sprout.Conn
	)
	const count = 3
	inNodes = make([]forest.Node, count)
	for i := range inNodes {
		inNodes[i] = randomIdentity(t)
	}
	conn := &LoopbackConn{}
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnAnnounce = func(s *sprout.Conn, m sprout.MessageID, nodes []forest.Node) error {
		outID = m
		outNodes = nodes
		return nil
	}
	inID, err = sconn.SendAnnounce(inNodes)
	if err != nil {
		t.Fatalf("failed to send response: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	} else if len(inNodes) != len(outNodes) {
		t.Fatalf("node list length mismatch, expected %d, got %d", len(inNodes), len(outNodes))
	}
	for i, node := range inNodes {
		if !node.Equals(outNodes[i]) {
			t.Fatalf("Node mismatch at index %d,\nin: %v\nout: %v", i, node, outNodes[i])
		}
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
		if _, err := sconn.SendVersion(); err != nil {
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
