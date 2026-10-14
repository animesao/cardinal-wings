package server

import (
	"sync"
	"testing"
)

// TestSftpLeaseMissingContainer verifies the offline-mount path fails fast
// (not hangs) when the container does not exist, and that releasing an
// unknown id is a harmless no-op.
func TestSftpLeaseMissingContainer(t *testing.T) {
	l := &sftpLeases{active: map[string]*sftpLeaseEntry{}}

	if _, _, err := l.acquire("no-such-container-xyz"); err == nil {
		t.Fatal("acquire of a missing container must fail")
	}
	if len(l.active) != 0 {
		t.Fatalf("failed acquire leaked %d lease entries", len(l.active))
	}
	// Must not panic.
	l.release("no-such-container-xyz")
}

// TestSftpLeaseConcurrentAcquire hammers the lease from many goroutines to
// shake out lock-ordering deadlocks (fmMountMu -> lease mu) and data races on
// the active map / refcounts. Run with -race.
func TestSftpLeaseConcurrentAcquire(t *testing.T) {
	l := &sftpLeases{active: map[string]*sftpLeaseEntry{}}

	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, release, err := l.acquire("no-such-container-xyz")
			errs[i] = err
			if err == nil && release != nil {
				release()
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			t.Fatalf("worker %d: acquire of a missing container must fail", i)
		}
	}
	l.mu.Lock()
	leaked := len(l.active)
	l.mu.Unlock()
	if leaked != 0 {
		t.Fatalf("concurrent failed acquires leaked %d lease entries", leaked)
	}
}
