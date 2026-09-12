package proxy

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	Sleep(d time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	when time.Time
	ch   chan time.Time
}

func NewFakeClock(now time.Time) *FakeClock {
	return &FakeClock{now: now}
}

func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *FakeClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	f.mu.Lock()
	when := f.now.Add(d)
	if !when.After(f.now) {
		ch <- f.now
		f.mu.Unlock()
		return ch
	}
	f.waiters = append(f.waiters, fakeWaiter{when: when, ch: ch})
	f.mu.Unlock()
	return ch
}

func (f *FakeClock) Sleep(d time.Duration) {
	<-f.After(d)
}

func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	now := f.now
	var keep []fakeWaiter
	for _, w := range f.waiters {
		if !w.when.After(now) {
			select {
			case w.ch <- now:
			default:
			}
		} else {
			keep = append(keep, w)
		}
	}
	f.waiters = keep
	f.mu.Unlock()
}
