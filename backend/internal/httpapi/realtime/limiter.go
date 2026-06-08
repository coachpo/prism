package realtime

import (
	"strings"
	"sync"
)

const (
	maxRealtimeConnections           = 128
	maxRealtimeConnectionsPerSubject = 16
)

type realtimeLimiter struct {
	mu        sync.Mutex
	global    int
	bySubject map[string]int
}

func newRealtimeLimiter() *realtimeLimiter {
	return &realtimeLimiter{bySubject: map[string]int{}}
}

func (l *realtimeLimiter) Acquire(subject string) (func(), bool) {
	if l == nil {
		return func() {}, true
	}
	subject = strings.TrimSpace(subject)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.global >= maxRealtimeConnections {
		return nil, false
	}
	if subject != "" && l.bySubject[subject] >= maxRealtimeConnectionsPerSubject {
		return nil, false
	}
	l.global++
	if subject != "" {
		l.bySubject[subject]++
	}
	released := false
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if released {
			return
		}
		released = true
		if l.global > 0 {
			l.global--
		}
		if subject != "" {
			remaining := l.bySubject[subject] - 1
			if remaining > 0 {
				l.bySubject[subject] = remaining
			} else {
				delete(l.bySubject, subject)
			}
		}
	}, true
}
