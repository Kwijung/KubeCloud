package kubevirt

import "testing"

func TestConsoleSessionsReplaceAndRelease(t *testing.T) {
	var sessions consoleSessions
	if sessions.inUse("vm-1") {
		t.Fatal("no session yet")
	}
	first, prev := sessions.begin("vm-1")
	if prev != nil || !sessions.inUse("vm-1") {
		t.Fatalf("first session should be current, prev=%v", prev)
	}

	second, prev := sessions.begin("vm-1")
	if prev != first || !first.replaced.Load() {
		t.Fatal("a second session must mark the first one replaced")
	}
	if second.closeCode() != consoleCloseNormal || first.closeCode() != ConsoleReplacedCloseCode {
		t.Fatalf("close codes: first=%d second=%d", first.closeCode(), second.closeCode())
	}

	// The first session ending must not drop the second one.
	sessions.end("vm-1", first)
	if !sessions.inUse("vm-1") {
		t.Fatal("the second session is still connected")
	}
	sessions.end("vm-1", second)
	if sessions.inUse("vm-1") {
		t.Fatal("no session should remain")
	}
}

func TestConsoleSessionsAbortRestoresPrevious(t *testing.T) {
	var sessions consoleSessions
	first, _ := sessions.begin("vm-1")
	second, prev := sessions.begin("vm-1")
	// Dialing KubeVirt failed, so the first session was never cut off.
	sessions.abort("vm-1", second, prev)
	if first.replaced.Load() || first.closeCode() != consoleCloseNormal {
		t.Fatal("an aborted takeover must not mark the first session replaced")
	}
	sessions.end("vm-1", first)
	if sessions.inUse("vm-1") {
		t.Fatal("the first session was the current one again and has ended")
	}
}

func TestConsoleSessionsAbortAfterPreviousEnded(t *testing.T) {
	var sessions consoleSessions
	first, _ := sessions.begin("vm-1")
	second, prev := sessions.begin("vm-1")
	// The VM stopped while the second session was dialing: the first session ended, then the dial failed.
	sessions.end("vm-1", first)
	sessions.abort("vm-1", second, prev)
	if sessions.inUse("vm-1") {
		t.Fatal("an ended session must not become current again")
	}
}

func TestConsoleSessionsTwoFailedTakeovers(t *testing.T) {
	var sessions consoleSessions
	first, _ := sessions.begin("vm-1")
	second, prev2 := sessions.begin("vm-1")
	third, prev3 := sessions.begin("vm-1")
	sessions.abort("vm-1", second, prev2) // a newer takeover is current; nothing to restore yet
	sessions.abort("vm-1", third, prev3)
	if !sessions.inUse("vm-1") || first.replaced.Load() {
		t.Fatal("the first session is still connected and must be current and not replaced")
	}
	sessions.end("vm-1", first)
	if sessions.inUse("vm-1") {
		t.Fatal("no session should remain")
	}
}
