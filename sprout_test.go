package sprout_test

import (
	"bytes"
	"math/rand"
	"net"
	"testing"
	"time"

	"git.sr.ht/~whereswaldon/forest-go/fields"
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
	sconn.OnQueryAny = func(s *sprout.Conn, m sprout.MessageID, nodeType fields.NodeType, quantity int) error {
		outID = m
		outNodeType = nodeType
		outQuantity = quantity
		return nil
	}
	inID, err = sconn.SendQueryAny(inNodeType, inQuantity)
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
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return &fields.QualifiedHash{
		Descriptor: fields.HashDescriptor{
			Type:   fields.HashTypeSHA512,
			Length: fields.ContentLength(length),
		},
		Blob: fields.Blob(b),
	}
}

func randomAncestryRequest() sprout.AncestryRequest {
	return sprout.AncestryRequest{
		QualifiedHash: randomQualifiedHash(),
		Levels:        int(rand.Uint32()),
	}
}

func randomQualifiedHashSlice(count int) []*fields.QualifiedHash {
	out := make([]*fields.QualifiedHash, count)
	for i := 0; i < count; i++ {
		out[i] = randomQualifiedHash()
	}
	return out
}

func randomAncestryRequestSlice(count int) []sprout.AncestryRequest {
	out := make([]sprout.AncestryRequest, count)
	for i := 0; i < count; i++ {
		out[i] = randomAncestryRequest()
	}
	return out
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
		inID, outID     sprout.MessageID
		inReqs, outReqs []sprout.AncestryRequest
		err             error
		sconn           *sprout.Conn
	)
	inReqs = randomAncestryRequestSlice(10)

	conn := new(LoopbackConn)
	sconn, err = sprout.NewConn(conn)
	if err != nil {
		t.Fatalf("failed to construct sprout.Conn: %v", err)
	}
	sconn.OnAncestry = func(s *sprout.Conn, m sprout.MessageID, reqs []sprout.AncestryRequest) error {
		outID = m
		outReqs = reqs
		return nil
	}
	inID, err = sconn.SendAncestry(inReqs...)
	if err != nil {
		t.Fatalf("failed to send ancestry: %v", err)
	}
	err = sconn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read ancestry: %v", err)
	}
	if inID != outID {
		t.Fatalf("id mismatch, got %d, expected %d", outID, inID)
	} else if len(inReqs) != len(outReqs) {
		t.Fatalf("request list length mismatch, expected %d, got %d", len(inReqs), len(outReqs))
	}
	for i, n := range inReqs {
		if !n.QualifiedHash.Equals(outReqs[i].QualifiedHash) {
			inString, _ := n.MarshalText()
			outString, _ := outReqs[i].MarshalText()
			t.Fatalf("node id mismatch, expected %s got %s", inString, outString)
		} else if n.Levels != outReqs[i].Levels {
			t.Fatalf("req level mismatch, expected %d got %d", n.Levels, outReqs[i].Levels)
		}
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
