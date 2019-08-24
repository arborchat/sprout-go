package sprout_test

import (
	"bytes"
	"net"
	"testing"
	"time"

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

func TestVersionHook(t *testing.T) {
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
