package sprout_test

import (
	"bytes"
	"encoding/base64"
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

func TestQueryAnyMessage(t *testing.T) {
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

func TestStatusMessage(t *testing.T) {
	var (
		inID, outID     sprout.MessageID
		inCode, outCode sprout.StatusCode
		err             error
		sconn           *sprout.Conn
	)
	inID = 5
	inCode = sprout.ErrorMalformed
	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnStatus = func(s *sprout.Conn, target sprout.MessageID, code sprout.StatusCode) error {
		outID = target
		outCode = code
		return nil
	}
	err = sconn.SendStatus(inID, inCode)
	if err != nil {
		t.Fatalf("failed to send error: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read error: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	} else if inCode != outCode {
		t.Fatalf("error code mismatch, expected %d, got %d", inCode, outCode)
	}
}

func TestResponseMessage(t *testing.T) {
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
	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnResponse = func(s *sprout.Conn, m sprout.MessageID, nodes []forest.Node) error {
		outID = m
		outNodes = nodes
		return nil
	}
	err = sconn.SendResponse(inID, inNodes)
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
