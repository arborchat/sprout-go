package main

import (
	"git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
)

type MessageStore struct {
	store             forest.Store
	requests          chan func()
	nextSubscriberKey int
	subscribers       map[int]func(forest.Node)
}

var _ forest.Store = &MessageStore{}

// NewMessageStore creates a thread-safe storage structure for
// forest nodes by wrapping an existing store implementation
func NewMessageStore(store forest.Store) *MessageStore {
	m := &MessageStore{
		store:       store,
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
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		subscriptionID = m.nextSubscriberKey
		m.nextSubscriberKey++
		m.subscribers[subscriptionID] = handler
	}
	<-done
	return
}

func (m *MessageStore) UnsubscribeToNewMessages(subscriptionID int) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		if _, subscribed := m.subscribers[subscriptionID]; subscribed {
			delete(m.subscribers, subscriptionID)
		}
	}
	<-done
	return
}

func (m *MessageStore) CopyInto(s forest.Store) (err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		err = m.store.CopyInto(s)
	}
	<-done
	return
}

func (m *MessageStore) Get(id *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.Get(id)
	}
	<-done
	return
}

func (m *MessageStore) GetIdentity(id *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.GetIdentity(id)
	}
	<-done
	return
}

func (m *MessageStore) GetCommunity(id *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.GetCommunity(id)
	}
	<-done
	return
}

func (m *MessageStore) GetConversation(communityID, conversationID *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.GetConversation(communityID, conversationID)
	}
	<-done
	return
}

func (m *MessageStore) GetReply(communityID, conversationID, replyID *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.GetReply(communityID, conversationID, replyID)
	}
	<-done
	return
}

func (m *MessageStore) Children(id *fields.QualifiedHash) (ids []*fields.QualifiedHash, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		ids, err = m.store.Children(id)
	}
	<-done
	return
}

func (m *MessageStore) Recent(nodeType fields.NodeType, quantity int) (nodes []forest.Node, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		nodes, err = m.store.Recent(nodeType, quantity)
	}
	<-done
	return
}

// Add inserts a node into the underlying store. Importantly, this will send a notification
// of a new node to *all* subscribers. If the calling code is a subscriber, it will still
// be notified of the new node. To supress this, use AddAs() instead.
func (m *MessageStore) Add(node forest.Node) (err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		err = m.store.Add(node)
	}
	<-done
	return
}

// AddAs allows adding a node to the underlying store without being notified
// of it as a new node. The addedByID (subscription id returned from SubscribeToNewMessages)
// will not be notified of the new nodes, but all other subscribers will be.
func (m *MessageStore) AddAs(node forest.Node, addedByID int) (err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		err = m.store.Add(node)
		if err == nil {
			for subscriptionID, handler := range m.subscribers {
				if subscriptionID != addedByID {
					handler(node)
				}
			}
		}
	}
	<-done
	return
}

// Shut down the worker gorountine that powers this store. Subsequent
// calls to methods on this MessageStore have undefined behavior
func (m *MessageStore) Destroy() {
	close(m.requests)
}
