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

// Shut down the worker gorountine that powers this store. Subsequent
// calls to methods on this MessageStore have undefined behavior
func (m *MessageStore) Destroy() {
	close(m.requests)
}
