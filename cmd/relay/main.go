package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"time"

	"git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
	sprout "git.sr.ht/~whereswaldon/sprout-go"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	certpath := flag.String("certpath", "", "Location of the TLS public key (certificate file)")
	keypath := flag.String("keypath", "", "Location of the TLS private key (key file)")
	tlsPort := flag.Int("tls-port", 7777, "TLS listen port")
	flag.Parse()

	cert, err := tls.LoadX509KeyPair(*certpath, *keypath)
	if err != nil {
		log.Fatalf("Failed loading certs: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	tlsConfig.BuildNameToCertificate()

	address := fmt.Sprintf(":%d", *tlsPort)
	listener, err := tls.Listen("tcp", address, tlsConfig)
	if err != nil {
		log.Fatalf("Failed to start TLS listener on address %s: %v", address, err)
	}
	done := make(chan struct{})

	// Set up channel on which to send signal notifications.
	// We must use a buffered channel or risk missing the signal
	// if we're not ready to receive when the signal is sent.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	messages := NewMessageStore()

	go func() {
		workerCount := 0
		go func() {
			time.Sleep(time.Second)
			log.Printf("Launching test connection to verify basic functionality")
			conn, err := tls.Dial("tcp", address, &tls.Config{
				InsecureSkipVerify: true,
			})
			if err != nil {
				log.Printf("Test dial failed: %v", err)
				return
			}
			defer func() {
				if err := conn.Close(); err != nil {
					log.Printf("Failed to close test connection: %v", err)
					return
				}
				log.Printf("Closed test connection")
			}()
			sconn, err := sprout.NewConn(conn)
			if err != nil {
				log.Printf("Failed to create sprout conn from test dial: %v", err)
			}
			log.Printf("Sending version information on test connection")
			if _, err := sconn.SendVersion(); err != nil {
				log.Printf("Failed to send version information from test conn: %v", err)
			}
		}()
		for {
			log.Printf("Waiting for connections...")
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Failed accepting connection: %v", err)
				continue
			}
			config := &WorkerConfig{
				Done:         done,
				Conn:         conn,
				Logger:       log.New(log.Writer(), fmt.Sprintf("worker-%d ", workerCount), log.Flags()),
				MessageStore: messages,
			}
			go config.Run()
			log.Printf("Launched worker-%d to handle new connection", workerCount)
			workerCount++
			select {
			case <-done:
				log.Printf("Done channel closed")
				return
			default:
			}
		}
	}()
	// Block until a signal is received.
	<-c
	close(done)
}

type MessageStore struct {
	store             forest.Store
	requests          chan func()
	nextSubscriberKey int
	subscribers       map[int]func(forest.Node)
}

// NewMessageStore creates a thread-safe storage structure for
// forest nodes.
func NewMessageStore() *MessageStore {
	m := &MessageStore{
		store:       forest.NewMemoryStore(),
		requests:    make(chan func()),
		subscribers: make(map[int]func(forest.Node)),
	}
	go func() {
		for function := range m.requests {
			function()
		}
	}()
	return m
}

func (m *MessageStore) SubscribeToNewMessages(handler func(n forest.Node)) (subscriptionID int) {
	m.requests <- func() {
		subscriptionID = m.nextSubscriberKey
		m.nextSubscriberKey++
		m.subscribers[subscriptionID] = handler
	}
	return
}

func (m *MessageStore) UnsubscribeToNewMessages(subscriptionID int) {
	m.requests <- func() {
		if _, subscribed := m.subscribers[subscriptionID]; subscribed {
			delete(m.subscribers, subscriptionID)
		}
	}
	return
}

func (m *MessageStore) Size() (size int, err error) {
	m.requests <- func() {
		size, err = m.store.Size()
	}
	return
}

func (m *MessageStore) CopyInto(s forest.Store) (err error) {
	m.requests <- func() {
		err = m.store.CopyInto(s)
	}
	return
}

func (m *MessageStore) Get(id *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	m.requests <- func() {
		node, present, err = m.store.Get(id)
	}
	return
}

func (m *MessageStore) Add(node forest.Node) (err error) {
	m.requests <- func() {
		err = m.store.Add(node)
		if err == nil {
			for _, handler := range m.subscribers {
				handler(node)
			}
		}
	}
	return
}

type WorkerConfig struct {
	Done <-chan struct{}
	net.Conn
	*log.Logger
	ConnState
	*MessageStore
}

type ConnState struct {
	Communities map[*fields.QualifiedHash]struct{}
}

func (c *WorkerConfig) Run() {
	defer func() {
		if err := c.Close(); err != nil {
			c.Printf("Failed closing connection: %v", err)
			return
		}
		c.Printf("Closed network connection")
	}()
	defer c.Printf("Shutting down")
	conn, err := sprout.NewConn(c.Conn)
	if err != nil {
		c.Printf("Failed to create SproutConn: %v", err)
		return
	}
	c.Communities = make(map[*fields.QualifiedHash]struct{})
	conn.OnVersion = c.OnVersion
	conn.OnList = c.OnList
	conn.OnQuery = c.OnQuery
	conn.OnAncestry = c.OnAncestry
	conn.OnLeavesOf = c.OnLeavesOf
	conn.OnResponse = c.OnResponse
	conn.OnSubscribe = c.OnSubscribe
	conn.OnUnsubscribe = c.OnUnsubscribe
	conn.OnStatus = c.OnStatus
	conn.OnAnnounce = c.OnAnnounce
	for {
		if err := conn.ReadMessage(); err != nil {
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

func (c *WorkerConfig) OnVersion(s *sprout.Conn, messageID sprout.MessageID, major, minor int) error {
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

func (c *WorkerConfig) OnList(s *sprout.Conn, messageID sprout.MessageID, nodeType fields.NodeType, quantity int) error {
	return nil
}

func (c *WorkerConfig) OnQuery(s *sprout.Conn, messageID sprout.MessageID, nodeIds []*fields.QualifiedHash) error {
	return nil
}

func (c *WorkerConfig) OnAncestry(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash, levels int) error {
	return nil
}

func (c *WorkerConfig) OnLeavesOf(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash, quantity int) error {
	return nil
}

func (c *WorkerConfig) OnResponse(s *sprout.Conn, target sprout.MessageID, nodes []forest.Node) error {
	return nil
}

func (c *WorkerConfig) OnSubscribe(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("Error during subscribe: %w", err)
		}
	}()
	_, alreadySubscribed := c.ConnState.Communities[nodeID]
	if !alreadySubscribed {
		c.ConnState.Communities[nodeID] = struct{}{}
	}
	if err := s.SendStatus(messageID, sprout.StatusOk); err != nil {
		return fmt.Errorf("Failed to send okay status: %w", err)
	}
	return nil
}

func (c *WorkerConfig) OnUnsubscribe(s *sprout.Conn, messageID sprout.MessageID, nodeID *fields.QualifiedHash) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("Error during unsubscribe: %w", err)
		}
	}()
	_, subscribed := c.ConnState.Communities[nodeID]
	if subscribed {
		delete(c.ConnState.Communities, nodeID)
	}
	if err := s.SendStatus(messageID, sprout.StatusOk); err != nil {
		return fmt.Errorf("Failed to send okay status: %w", err)
	}
	return nil
}

func (c *WorkerConfig) OnStatus(s *sprout.Conn, messageID sprout.MessageID, code sprout.StatusCode) error {
	return nil
}

func (c *WorkerConfig) OnAnnounce(s *sprout.Conn, messageID sprout.MessageID, nodes []forest.Node) error {
	return nil
}
