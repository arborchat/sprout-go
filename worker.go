package sprout

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"time"

	"git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
)

type SubscribableStore interface {
	forest.Store
	SubscribeToNewMessages(handler func(n forest.Node)) (subscriptionID int)
	UnsubscribeToNewMessages(subscriptionID int)
	AddAs(node forest.Node, addedByID int) (err error)
}

type Worker struct {
	Done <-chan struct{}
	*Conn
	*log.Logger
	*Session
	SubscribableStore
	subscriptionID int
}

func NewWorker(done <-chan struct{}, conn net.Conn, store SubscribableStore) (*Worker, error) {
	w := &Worker{
		Done:              done,
		SubscribableStore: store,
		Logger:            log.New(log.Writer(), "", log.LstdFlags|log.Lshortfile),
	}
	var err error
	w.Conn, err = NewConn(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create sprout conn: %w", err)
	}
	w.Session = NewSession()
	w.Conn.OnVersion = w.OnVersion
	w.Conn.OnList = w.OnList
	w.Conn.OnQuery = w.OnQuery
	w.Conn.OnAncestry = w.OnAncestry
	w.Conn.OnLeavesOf = w.OnLeavesOf
	w.Conn.OnSubscribe = w.OnSubscribe
	w.Conn.OnUnsubscribe = w.OnUnsubscribe
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
			var unsolicitedErr UnsolicitedMessageError
			if errors.As(err, &unsolicitedErr) {
				c.Printf("ignoring unsolicited message responding to %d", unsolicitedErr.MessageID)
			} else {
				c.Printf("failed to read sprout message: %v", err)
				return
			}
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
	switch n := node.(type) {
	// TODO: DRY this out
	case *forest.Identity:
		if err := c.SendAnnounce([]forest.Node{n}, makeTicker(time.Second*5)); err != nil {
			c.Printf("Error announcing new identity: %v", err)
		}
	case *forest.Community:
		if err := c.SendAnnounce([]forest.Node{n}, makeTicker(time.Second*5)); err != nil {
			c.Printf("Error announcing new community: %v", err)
		}
	case *forest.Reply:
		if c.IsSubscribed(&n.CommunityID) {
			if err := c.SendAnnounce([]forest.Node{n}, time.NewTicker(time.Second*10).C); err != nil {
				c.Printf("Error announcing new reply: %v", err)
			}
		}
	default:
		log.Printf("Unknown node type: %T", n)
	}
}

func (c *Worker) OnVersion(s *Conn, messageID MessageID, major, minor int) error {
	c.Printf("Received version: id:%d major:%d minor:%d", messageID, major, minor)
	if major < CurrentMajor {
		if err := s.SendStatus(messageID, ErrorProtocolTooOld); err != nil {
			return fmt.Errorf("Failed to send protocol too old message: %w", err)
		}
		return nil
	}
	if major > CurrentMajor {
		if err := s.SendStatus(messageID, ErrorProtocolTooNew); err != nil {
			return fmt.Errorf("Failed to send protocol too new message: %w", err)
		}
		return nil
	}
	if err := s.SendStatus(messageID, StatusOk); err != nil {
		return fmt.Errorf("Failed to send okay message: %w", err)
	}
	return nil
}

func (c *Worker) OnList(s *Conn, messageID MessageID, nodeType fields.NodeType, quantity int) error {
	// requires better iteration on Store types
	nodes, err := c.SubscribableStore.Recent(nodeType, quantity)
	if err != nil {
		return fmt.Errorf("failed listing recent nodes of type %d: %w", nodeType, err)
	}
	return s.SendResponse(messageID, nodes)
}

func (c *Worker) OnQuery(s *Conn, messageID MessageID, nodeIds []*fields.QualifiedHash) error {
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

func (c *Worker) OnAncestry(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash, levels int) error {
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
	// reverse the order
	sort.Slice(ancestors, func(i, j int) bool {
		return ancestors[i].TreeDepth() < ancestors[j].TreeDepth()
	})
	return s.SendResponse(messageID, ancestors)
}

func (c *Worker) OnLeavesOf(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash, quantity int) error {
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
	if len(leaves) > quantity {
		leaves = leaves[:quantity]
	}
	return s.SendResponse(messageID, leaves)
}

func (c *Worker) OnSubscribe(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("Error during subscribe: %w", err)
		}
	}()
	c.Subscribe(nodeID)
	if err := s.SendStatus(messageID, StatusOk); err != nil {
		return fmt.Errorf("Failed to send okay status: %w", err)
	}
	return nil
}

func (c *Worker) OnUnsubscribe(s *Conn, messageID MessageID, nodeID *fields.QualifiedHash) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("Error during unsubscribe: %w", err)
		}
	}()
	c.Unsubscribe(nodeID)
	if err := s.SendStatus(messageID, StatusOk); err != nil {
		return fmt.Errorf("Failed to send okay status: %w", err)
	}
	return nil
}

func (c *Worker) OnAnnounce(s *Conn, messageID MessageID, nodes []forest.Node) error {
	var err error
	for _, node := range nodes {
		c.Printf("Handling announcement for node %s", node.ID().String())
		switch n := node.(type) {
		case *forest.Identity:
			err = c.SubscribableStore.AddAs(n, c.subscriptionID)
		case *forest.Community:
			err = c.SubscribableStore.AddAs(n, c.subscriptionID)
		case *forest.Reply:
			if c.Session.IsSubscribed(&n.CommunityID) {
				err = c.SubscribableStore.AddAs(n, c.subscriptionID)
			} else {
				err = fmt.Errorf("received annoucement for reply %s in non-subscribed community %s", n.ID().String(), n.CommunityID.String())
			}
		default:
			err = fmt.Errorf("Unknown node type announced: %T", node)
		}
	}
	if err != nil {
		return fmt.Errorf("Failed handling announce node: %w", err)
	}
	return s.SendStatus(messageID, StatusOk)
}

// BootstrapLocalStore is a utility method for loading all available
// content from the peer on the other end of the sprout connection.
// It will
//
// - discover all communities
// - fetch the signing identities of those communities
// - validate and insert those identities and communities into the
//   worker's store
// - subscribe to all of those communities
// - fetch all leaves of those communities
// - fetch the ancestry of each leaf and validate it (fetching identities as necessary), inserting nodes that pass valdiation into the store
func (c *Worker) BootstrapLocalStore(maxCommunities int, perRequestTimeout time.Duration) {
	communities, err := c.SendList(fields.NodeTypeCommunity, maxCommunities, makeTicker(perRequestTimeout))
	if err != nil {
		c.Printf("Failed listing peer communities: %v", err)
		return
	}
	for _, node := range communities.Nodes {
		community, isCommunity := node.(*forest.Community)
		if !isCommunity {
			c.Printf("Got response in community list that isn't a community: %s", node.ID().String())
			continue
		}
		if err := c.ensureAuthorAvailable(community, perRequestTimeout); err != nil {
			c.Printf("Couldn't fetch author information for node %s: %v", community.ID().String(), err)
			continue
		}
		if err := c.AddAs(community, c.subscriptionID); err != nil {
			c.Printf("Couldn't add community %s to store: %v", community.ID().String(), err)
			continue
		}
		if err := c.SendSubscribe(community, makeTicker(perRequestTimeout)); err != nil {
			c.Printf("Couldn't subscribe to community %s", community.ID().String())
			continue
		}
		c.Subscribe(community.ID())
		c.Printf("Subscribed to %s", community.ID().String())
		if err := c.fetchFullTree(community, maxCommunities, perRequestTimeout); err != nil {
			c.Printf("Couldn't fetch message tree rooted at community %s: %v", community.ID().String(), err)
			continue
		}
	}
}

func (c *Worker) fetchFullTree(root forest.Node, maxNodes int, perRequestTimeout time.Duration) error {
	leafList, err := c.SendLeavesOf(root.ID(), maxNodes, makeTicker(perRequestTimeout))
	if err != nil {
		return fmt.Errorf("couldn't fetch leaves of node %s: %v", root.ID().String(), err)
	}
	for _, leaf := range leafList.Nodes {
		if _, alreadyInStore, err := c.Get(leaf.ID()); err != nil {
			return fmt.Errorf("failed checking if we already have leaf node %s: %w", leaf.ID().String(), err)
		} else if alreadyInStore {
			continue
		}
		ancestry, err := c.SendAncestry(leaf.ID(), int(leaf.TreeDepth()), makeTicker(perRequestTimeout))
		if err != nil {
			return fmt.Errorf("couldn't fetch ancestry of node %s: %v", leaf.ID().String(), err)
		}
		sort.Slice(ancestry.Nodes, func(i, j int) bool {
			return ancestry.Nodes[i].TreeDepth() < ancestry.Nodes[j].TreeDepth()
		})
		ancestry.Nodes = append(ancestry.Nodes, leaf)
		for _, ancestor := range ancestry.Nodes {
			if err := c.ensureAuthorAvailable(ancestor, perRequestTimeout); err != nil {
				return fmt.Errorf("couldn't fetch author for node %s: %w", ancestor.ID().String(), err)
			}
			if err := ancestor.ValidateDeep(c.SubscribableStore); err != nil {
				return fmt.Errorf("couldn't validate node %s: %w", ancestor.ID().String(), err)
			}
			if err := c.AddAs(ancestor, c.subscriptionID); err != nil {
				return fmt.Errorf("couldn't add node %s to store: %w", ancestor.ID().String(), err)
			}
		}
	}
	return nil
}

func makeTicker(duration time.Duration) <-chan time.Time {
	return time.NewTicker(duration).C
}

func (c *Worker) ensureAuthorAvailable(node forest.Node, perRequestTimeout time.Duration) error {
	var authorID *fields.QualifiedHash
	switch n := node.(type) {
	case *forest.Identity:
		authorID = &n.Author
	case *forest.Community:
		authorID = &n.Author
	case *forest.Reply:
		authorID = &n.Author
	default:
		return fmt.Errorf("unsupported type in ensureAuthorAvailable: %T", n)
	}
	_, inStore, err := c.GetIdentity(authorID)
	if err != nil {
		return fmt.Errorf("failed looking for author id %s in store: %w", authorID.String(), err)
	}
	if inStore {
		return nil
	}
	response, err := c.SendQuery([]*fields.QualifiedHash{authorID}, makeTicker(perRequestTimeout))
	if err != nil {
		return fmt.Errorf("failed querying for author %s: %w", authorID.String(), err)
	}
	if len(response.Nodes) != 1 {
		return fmt.Errorf("query for single author id %s returned %d nodes", authorID.String(), len(response.Nodes))
	}
	author := response.Nodes[0]
	if err := author.ValidateDeep(c.SubscribableStore); err != nil {
		return fmt.Errorf("unable to validate author %s: %w", author.ID().String(), err)
	}
	if err := c.AddAs(author, c.subscriptionID); err != nil {
		return fmt.Errorf("failed inserting new valid author %s into store: %w", author.ID().String(), err)
	}
	return nil
}
