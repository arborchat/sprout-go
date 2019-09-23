package main

import (
	"fmt"
	"log"
	"net"

	"git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
	sprout "git.sr.ht/~whereswaldon/sprout-go"
)

type Worker struct {
	Done <-chan struct{}
	*sprout.Conn
	*log.Logger
	*Session
	*MessageStore
	subscriptionID int
}

func NewWorker(done <-chan struct{}, conn net.Conn, store *MessageStore) (*Worker, error) {
	w := &Worker{
		Done:         done,
		MessageStore: store,
	}
	var err error
	w.Conn, err = sprout.NewConn(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create sprout conn: %w", err)
	}
	w.Session = NewSession()
	w.Conn.OnVersion = w.OnVersion
	w.Conn.OnList = w.OnList
	w.Conn.OnQuery = w.OnQuery
	w.Conn.OnAncestry = w.OnAncestry
	w.Conn.OnLeavesOf = w.OnLeavesOf
	w.Conn.OnResponse = w.OnResponse
	w.Conn.OnSubscribe = w.OnSubscribe
	w.Conn.OnUnsubscribe = w.OnUnsubscribe
	w.Conn.OnStatus = w.OnStatus
	w.Conn.OnAnnounce = w.OnAnnounce
	return w, nil
}

func (c *Worker) Run() {
	defer func() {
		if err := c.Conn.Conn.Close(); err != nil {
			c.Printf("Failed closing connection: %v", err)
			return
		}
		c.Printf("Closed network connection")
	}()
	defer c.Printf("Shutting down")
	c.subscriptionID = c.MessageStore.SubscribeToNewMessages(c.HandleNewNode)
	defer c.MessageStore.UnsubscribeToNewMessages(c.subscriptionID)
	for {
		if err := c.ReadMessage(); err != nil {
			c.Printf("failed to read sprout message: %v", err)
			return
		}
		select {
		case <-c.Done:
			c.Printf("Done channel closed")
			return
		default:
		}
	}
}

func (c *Worker) HandleNewNode(node forest.Node) {
	log.Printf("Got new node: %v", node)
	switch n := node.(type) {
	case *forest.Identity:
	case *forest.Community:
	case *forest.Reply:
		if c.IsSubscribed(&n.CommunityID) {
			if _, err := c.SendAnnounce([]forest.Node{n}); err != nil {
				c.Printf("Error announcing new reply: %v", err)
			}
		}
	default:
		log.Printf("Unknown node type: %T", n)
	}
}

func (c *Worker) OnVersion(s *sprout.Conn, messageID sprout.MessageID, major, minor int) error {
	c.Printf("Received version: id:%d major:%d minor:%d", messageID, major, minor)
	if major < sprout.CurrentMajor {
		if err := s.SendStatus(messageID, sprout.ErrorProtocolTooOld); err != nil {
			return fmt.Errorf("Failed to send protocol too old message: %w", err)
		}
		return nil
	}
	if major > sprout.CurrentMajor {
		if err := s.SendStatus(messageID, sprout.ErrorProtocolTooNew); err != nil {
			return fmt.Errorf("Failed to send protocol too new message: %w", err)
		}
		return nil
	}
	if err := s.SendStatus(messageID, sprout.StatusOk); err != nil {
		return fmt.Errorf("Failed to send okay message: %w", err)
	}
	return nil
}

func (c *Worker) OnList(s *sprout.Conn, messageID sprout.MessageID, nodeType fields.NodeType, quantity int) error {
	// requires better iteration on Store types
	return nil
}

func (c *Worker) OnQuery(s *sprout.Conn, messageID sprout.MessageID, nodeIds []*fields.QualifiedHash) error {
	results := make([]forest.Node, 0, len(nodeIds))
	for _, id := range nodeIds {
		node, present, err := c.MessageStore.Get(id)
		if err != nil {
			return fmt.Errorf("failed checking for node %v in store: %w", id, err)
		} else if present {
			results = append(results, node)
		}
	}
	return s.SendResponse(messageID, results)
}

func (c *Worker) OnAncestry(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash, levels int) error {
	return nil
}

func (c *Worker) OnLeavesOf(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash, quantity int) error {
	return nil
}

func (c *Worker) OnResponse(s *sprout.Conn, target sprout.MessageID, nodes []forest.Node) error {
	return nil
}

func (c *Worker) OnSubscribe(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("Error during subscribe: %w", err)
		}
	}()
	c.Subscribe(nodeID)
	if err := s.SendStatus(messageID, sprout.StatusOk); err != nil {
		return fmt.Errorf("Failed to send okay status: %w", err)
	}
	return nil
}

func (c *Worker) OnUnsubscribe(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("Error during unsubscribe: %w", err)
		}
	}()
	c.Unsubscribe(nodeID)
	if err := s.SendStatus(messageID, sprout.StatusOk); err != nil {
		return fmt.Errorf("Failed to send okay status: %w", err)
	}
	return nil
}

func (c *Worker) OnStatus(s *sprout.Conn, messageID sprout.MessageID, code sprout.StatusCode) error {
	c.Printf("Received status %d for message %d", code, messageID)
	return nil
}

func (c *Worker) OnAnnounce(s *sprout.Conn, messageID sprout.MessageID, nodes []forest.Node) error {
	var err error
	for _, node := range nodes {
		switch n := node.(type) {
		case *forest.Identity:
			err = c.MessageStore.Add(n, c.subscriptionID)
		case *forest.Community:
			err = c.MessageStore.Add(n, c.subscriptionID)
		case *forest.Reply:
			if c.Session.IsSubscribed(&n.CommunityID) {
				err = c.MessageStore.Add(n, c.subscriptionID)
			}
		default:
			err = fmt.Errorf("Unknown node type announced: %T", node)
		}
	}
	if err != nil {
		return fmt.Errorf("Failed handling announce node: %w", err)
	}
	return s.SendStatus(messageID, sprout.StatusOk)
}
