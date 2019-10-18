package main

import (
	forest "git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
)

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

func (m *MessageStore) GetIdentity(id *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	m.requests <- func() {
		node, present, err = m.store.GetIdentity(id)
	}
	return
}

func (m *MessageStore) GetCommunity(id *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	m.requests <- func() {
		node, present, err = m.store.GetCommunity(id)
	}
	return
}

func (m *MessageStore) GetConversation(communityID, conversationID *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	m.requests <- func() {
		node, present, err = m.store.GetConversation(communityID, conversationID)
	}
	return
}

func (m *MessageStore) GetReply(communityID, conversationID, replyID *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	m.requests <- func() {
		node, present, err = m.store.GetReply(communityID, conversationID, replyID)
	}
	return
}

func (m *MessageStore) Children(parent *fields.QualifiedHash) (children []*fields.QualifiedHash, err error) {
	m.requests <- func() {
		children, err = m.store.Children(parent)
	}
	return
}

func (m *MessageStore) Recent(nodeType fields.NodeType, quantity int) (recentNodes []forest.Node, err error) {
	m.requests <- func() {
		recentNodes, err = m.store.Recent(nodeType, quantity)
	}
	return
}

func (m *MessageStore) Add(node forest.Node, addedByID int) (err error) {
	m.requests <- func() {
		err = m.store.Add(node)
		if err == nil {
			for subscriptionID, handler := range m.subscribers {
				if subscriptionID != addedByID {
					handler(node)
				}
			}
		}
	}
	return
}

// Shut down the worker gorountine that powers this store. Subsequent
// calls to methods on this MessageStore have undefined behavior
func (m *MessageStore) Destroy() {
	close(m.requests)
}
