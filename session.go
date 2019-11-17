package sprout

import (
	"sync"

	"git.sr.ht/~whereswaldon/forest-go/fields"
)

// Session stores the state of a sprout connection between hosts,
// which is currently just the subscribed community set.
type Session struct {
	sync.RWMutex
	Communities map[*fields.QualifiedHash]struct{}
}

func NewSession() *Session {
	return &Session{
		Communities: make(map[*fields.QualifiedHash]struct{}),
	}
}

func (c *Session) Subscribe(communityID *fields.QualifiedHash) {
	c.Lock()
	defer c.Unlock()

	c.Communities[communityID] = struct{}{}
}

func (c *Session) IsSubscribed(communityID *fields.QualifiedHash) bool {
	c.RLock()
	defer c.RUnlock()

	for community := range c.Communities {
		if community.Equals(communityID) {
			return true
		}
	}
	return false
}

func (c *Session) Unsubscribe(communityID *fields.QualifiedHash) {
	c.Lock()
	defer c.Unlock()

	for community := range c.Communities {
		if community.Equals(communityID) {
			delete(c.Communities, community)
			return
		}
	}
}
