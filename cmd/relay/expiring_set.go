package main

import (
	"sync"
	"time"
)

type record struct {
	expiration time.Time
}

// ExpiringSet is a string set in which elements will automatically be deleted
// after a period of time elapses.
type ExpiringSet struct {
	sync.RWMutex
	elements map[string]record
}

// NewExpiringSet constructs a new expiring set
func NewExpiringSet() *ExpiringSet {
	return &ExpiringSet{
		elements: make(map[string]record),
	}
}

// Add inserts an element that will be retained in the set for the given
// lifetime.
func (s *ExpiringSet) Add(element string, lifetime time.Duration) {
	s.purge()
	s.Lock()
	defer s.Unlock()
	s.elements[element] = record{time.Now().Add(lifetime)}
}

// purge removes all elements older than their expiration. It locks the set
// while doing so, and should not be called when the set is already locked.
func (s *ExpiringSet) purge() {
	now := time.Now()
	s.Lock()
	defer s.Unlock()
	for key, value := range s.elements {
		if value.expiration.Before(now) {
			delete(s.elements, key)
		}
	}
}

// Has returns whether the given element is still in the set.
func (s *ExpiringSet) Has(element string) bool {
	s.purge()
	s.RLock()
	defer s.RUnlock()
	_, has := s.elements[element]
	return has
}
