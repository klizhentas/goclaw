package conversation

import (
	"sync"
	"testing"
	"time"
)

func TestLockMap_SerializesPerConversation(t *testing.T) {
	locks := NewLockMap()

	var wg sync.WaitGroup
	startSecond := make(chan struct{})
	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	secondEntered := make(chan struct{})

	wg.Add(2)
	go func() {
		defer wg.Done()
		unlock := locks.Lock("same")
		close(firstEntered)
		<-firstRelease
		unlock()
	}()

	go func() {
		defer wg.Done()
		<-startSecond
		unlock := locks.Lock("same")
		close(secondEntered)
		unlock()
	}()

	<-firstEntered
	close(startSecond)

	select {
	case <-secondEntered:
		t.Fatal("second goroutine entered critical section before first released lock")
	case <-time.After(40 * time.Millisecond):
	}

	close(firstRelease)

	select {
	case <-secondEntered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second goroutine did not acquire lock after release")
	}

	wg.Wait()
}
