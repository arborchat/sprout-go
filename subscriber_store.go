package sprout

import (
	"git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/fields"
)

// Subscription is an identifier for a particular handler function within
// a SubscriberStore. It can be provided to delete a handler function or to
// suppress notifications to the corresponding handler.
type Subscription uint

// the zero subscription is never used
const neverAssigned = 0
const firstSubscription = 1

// SubscriberStore is a wrapper type that extends the forest.Store interface
// with the observer pattern. Code can subscribe for updates each time a
// node is inserted into the store using Add or AddAs.
type SubscriberStore struct {
	store                                 forest.Store
	requests                              chan func()
	nextSubscriberKey                     Subscription
	postAddSubscribers, preAddSubscribers map[Subscription]func(forest.Node)
}

var _ forest.Store = &SubscriberStore{}

// NewMessageStore creates a thread-safe storage structure for
// forest nodes by wrapping an existing store implementation
func NewSubscriberStore(store forest.Store) *SubscriberStore {
	m := &SubscriberStore{
		store:              store,
		requests:           make(chan func()),
		nextSubscriberKey:  firstSubscription,
		postAddSubscribers: make(map[Subscription]func(forest.Node)),
		preAddSubscribers:  make(map[Subscription]func(forest.Node)),
	}
	go func() {
		for function := range m.requests {
			function()
		}
	}()
	return m
}

// SubscribeToNewMessages establishes the given function as a handler to be
// invoked on each node added to the store. The returned subscription ID
// can be used to unsubscribe later, as well as to supress notifications
// with AddAs().
//
// Handler functions are invoked synchronously on the same goroutine that invokes
// Add() or AddAs(), and should not block. If long-running code is needed in a
// handler, launch a new goroutine.
func (m *SubscriberStore) SubscribeToNewMessages(handler func(n forest.Node)) (subscriptionID Subscription) {
	return m.subscribeInMap(m.postAddSubscribers, handler)
}

// PresubscribeToNewMessages establishes the given function as a handler to be
// invoked on each node added to the store. The returned subscription ID
// can be used to unsubscribe later, as well as to supress notifications
// with AddAs(). The handler function will be invoked *before* nodes are
// inserted into the store instead of after (like a normal Subscribe).
//
// Handler functions are invoked synchronously on the same goroutine that invokes
// Add() or AddAs(), and should not block. If long-running code is needed in a
// handler, launch a new goroutine.
func (m *SubscriberStore) PresubscribeToNewMessages(handler func(n forest.Node)) (subscriptionID Subscription) {
	return m.subscribeInMap(m.preAddSubscribers, handler)
}

func (m *SubscriberStore) subscribeInMap(targetMap map[Subscription]func(forest.Node), handler func(n forest.Node)) (subscriptionID Subscription) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		subscriptionID = m.nextSubscriberKey
		m.nextSubscriberKey++
		// handler unsigned overflow
		// TODO: ensure subscription reuse can't occur
		if m.nextSubscriberKey == neverAssigned {
			m.nextSubscriberKey = firstSubscription
		}
		targetMap[subscriptionID] = handler
	}
	<-done
	return
}

// UnsubscribeToNewMessages removes the handler for a given subscription from
// the store.
func (m *SubscriberStore) UnsubscribeToNewMessages(subscriptionID Subscription) {
	m.unsubscribeInMap(m.postAddSubscribers, subscriptionID)
}

// UnpresubscribeToNewMessages removes the handler for a given subscription from
// the store.
func (m *SubscriberStore) UnpresubscribeToNewMessages(subscriptionID Subscription) {
	m.unsubscribeInMap(m.preAddSubscribers, subscriptionID)
}

func (m *SubscriberStore) unsubscribeInMap(targetMap map[Subscription]func(forest.Node), subscriptionID Subscription) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		if _, subscribed := targetMap[subscriptionID]; subscribed {
			delete(targetMap, subscriptionID)
		}
	}
	<-done
	return
}

func (m *SubscriberStore) CopyInto(s forest.Store) (err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		err = m.store.CopyInto(s)
	}
	<-done
	return
}

func (m *SubscriberStore) Get(id *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.Get(id)
	}
	<-done
	return
}

func (m *SubscriberStore) GetIdentity(id *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.GetIdentity(id)
	}
	<-done
	return
}

func (m *SubscriberStore) GetCommunity(id *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.GetCommunity(id)
	}
	<-done
	return
}

func (m *SubscriberStore) GetConversation(communityID, conversationID *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.GetConversation(communityID, conversationID)
	}
	<-done
	return
}

func (m *SubscriberStore) GetReply(communityID, conversationID, replyID *fields.QualifiedHash) (node forest.Node, present bool, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		node, present, err = m.store.GetReply(communityID, conversationID, replyID)
	}
	<-done
	return
}

func (m *SubscriberStore) Children(id *fields.QualifiedHash) (ids []*fields.QualifiedHash, err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		ids, err = m.store.Children(id)
	}
	<-done
	return
}

func (m *SubscriberStore) Recent(nodeType fields.NodeType, quantity int) (nodes []forest.Node, err error) {
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
func (m *SubscriberStore) Add(node forest.Node) (err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		m.notifySubscribed(m.preAddSubscribers, node, neverAssigned)
		if err = m.store.Add(node); err == nil {
			m.notifySubscribed(m.postAddSubscribers, node, neverAssigned)
		}
	}
	<-done
	return
}

// AddAs allows adding a node to the underlying store without being notified
// of it as a new node. The addedByID (subscription id returned from SubscribeToNewMessages)
// will not be notified of the new nodes, but all other subscribers will be.
func (m *SubscriberStore) AddAs(node forest.Node, addedByID Subscription) (err error) {
	done := make(chan struct{})
	m.requests <- func() {
		defer close(done)
		m.notifySubscribed(m.preAddSubscribers, node, addedByID)
		if err = m.store.Add(node); err == nil {
			m.notifySubscribed(m.postAddSubscribers, node, addedByID)
		}
	}
	<-done
	return
}

// notifySubscribed runs all of the subscription handlers in new goroutines with
// the provided node as input to each handler.
func (m *SubscriberStore) notifySubscribed(targetMap map[Subscription]func(forest.Node), node forest.Node, ignore Subscription) {
	for subscriptionID, handler := range targetMap {
		if subscriptionID != ignore {
			handler(node)
		}
	}
}

// Shut down the worker gorountine that powers this store. Subsequent
// calls to methods on this MessageStore have undefined behavior
func (m *SubscriberStore) Destroy() {
	close(m.requests)
}
