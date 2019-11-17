package main

import (
	"fmt"
	"log"
	"net"

	"git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
	sprout "git.sr.ht/~whereswaldon/sprout-go"
)

type SubscribableStore interface {
	forest.Store
	SubscribeToNewMessages(handler func(n forest.Node)) (subscriptionID int)
	UnsubscribeToNewMessages(subscriptionID int)
	AddAs(node forest.Node, addedByID int) (err error)
}

type Worker struct {
	Done <-chan struct{}
	*sprout.Conn
	*log.Logger
	*Session
	SubscribableStore
	subscriptionID int
}

func NewWorker(done <-chan struct{}, conn net.Conn, store SubscribableStore) (*Worker, error) {
	w := &Worker{
		Done:              done,
		SubscribableStore: store,
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
	c.subscriptionID = c.SubscribableStore.SubscribeToNewMessages(c.HandleNewNode)
	defer c.SubscribableStore.UnsubscribeToNewMessages(c.subscriptionID)
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
		// shouldn't just announce random user ids unsolicted
	case *forest.Community:
		// maybe we should announce new communities?
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
	nodes, err := c.SubscribableStore.Recent(nodeType, quantity)
	if err != nil {
		return fmt.Errorf("failed listing recent nodes of type %d: %w", nodeType, err)
	}
	return s.SendResponse(messageID, nodes)
}

func (c *Worker) OnQuery(s *sprout.Conn, messageID sprout.MessageID, nodeIds []*fields.QualifiedHash) error {
	results := make([]forest.Node, 0, len(nodeIds))
	for _, id := range nodeIds {
		node, present, err := c.SubscribableStore.Get(id)
		if err != nil {
			return fmt.Errorf("failed checking for node %v in store: %w", id, err)
		} else if present {
			results = append(results, node)
		}
	}
	return s.SendResponse(messageID, results)
}

func (c *Worker) OnAncestry(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash, levels int) error {
	ancestors := make([]forest.Node, 0, 1024)
	currentNode, known, err := c.SubscribableStore.Get(nodeID)
	if err != nil {
		return fmt.Errorf("failed looking for node %v: %w", nodeID, err)
	} else if !known {
		return fmt.Errorf("asked for ancestry of unknown node %v", nodeID)
	}
	for i := 0; i < levels; i++ {
		if currentNode.ParentID().Equals(fields.NullHash()) {
			// no parent, we're done
			break
		}
		parentNode, known, err := c.SubscribableStore.Get(currentNode.ParentID())
		if err != nil {
			return fmt.Errorf("couldn't look up node with id %v (parent of %v): %w", currentNode.ParentID(), currentNode.ID(), err)
		} else if !known {
			// we don't know any more ancestry, so we're done
			break
		}
		ancestors = append(ancestors, parentNode)
		currentNode = parentNode
	}
	return s.SendResponse(messageID, ancestors)
}

func (c *Worker) OnLeavesOf(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash, quantity int) error {
	descendants := make([]*fields.QualifiedHash, 0, 1024)
	descendants = append(descendants, nodeID)
	leaves := make([]forest.Node, 0, 1024)
	seen := make(map[string]struct{})
	for len(descendants) > 0 {
		current := descendants[0]
		descendants = descendants[1:]
		seen[current.String()] = struct{}{}
		children, err := c.SubscribableStore.Children(current)
		if err != nil {
			return fmt.Errorf("failed fetching children for %v: %w", current, err)
		}
		if len(children) == 0 {
			node, has, err := c.SubscribableStore.Get(current)
			if err != nil {
				return fmt.Errorf("failed fetching node for %v: %w", current, err)
			} else if !has {
				// not sure what to do here
				continue
			}
			leaves = append(leaves, node)
		}
		for _, child := range children {
			if _, alreadySeen := seen[child.String()]; !alreadySeen {
				descendants = append(descendants, child)
			}
		}
	}
	return s.SendResponse(messageID, leaves)
}

func (c *Worker) OnResponse(s *sprout.Conn, target sprout.MessageID, nodes []forest.Node) error {
	for _, node := range nodes {
		if err := c.SubscribableStore.AddAs(node, c.subscriptionID); err != nil {
			return fmt.Errorf("failed to add node to store: %w", err)
		}
	}
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
			err = c.SubscribableStore.AddAs(n, c.subscriptionID)
		case *forest.Community:
			err = c.SubscribableStore.AddAs(n, c.subscriptionID)
		case *forest.Reply:
			if c.Session.IsSubscribed(&n.CommunityID) {
				err = c.SubscribableStore.AddAs(n, c.subscriptionID)
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
