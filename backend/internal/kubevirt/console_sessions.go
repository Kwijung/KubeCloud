package kubevirt

import (
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// ConsoleReplacedCloseCode is the WebSocket close code sent to a console session that another session took
// over. KubeVirt serves one serial console connection per VM and drops the old one when a new one connects;
// the code tells the console why it was cut off.
const ConsoleReplacedCloseCode = 4001

const consoleCloseNormal = websocket.CloseNormalClosure

// consoleSession is one browser connected to a VM's serial console through this API.
type consoleSession struct {
	replaced atomic.Bool
	// previous is the session this one took over from; done is set when the session ends or fails to
	// connect. Both are guarded by consoleSessions.mu.
	previous *consoleSession
	done     bool
}

func (s *consoleSession) closeCode() int {
	if s.replaced.Load() {
		return ConsoleReplacedCloseCode
	}
	return consoleCloseNormal
}

// consoleSessions tracks the current console session of each VM. Every console connection goes through this
// API (a single replica), so it knows whether a console is in use; connections made around it, such as
// virtctl, are not seen.
type consoleSessions struct {
	mu      sync.Mutex
	current map[string]*consoleSession
}

// begin makes a new session current and marks the previous one, if any, as replaced. It must be called
// before dialing KubeVirt, which cuts the previous connection off as soon as the new one connects.
func (c *consoleSessions) begin(vmID string) (session, previous *consoleSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		c.current = map[string]*consoleSession{}
	}
	previous = c.current[vmID]
	if previous != nil {
		previous.replaced.Store(true)
	}
	session = &consoleSession{previous: previous}
	c.current[vmID] = session
	return session, previous
}

// abort undoes begin when the new session could not connect. The newest earlier session that is still
// connected becomes current again and is no longer marked replaced; sessions that ended meanwhile, or
// takeovers that also failed, are skipped.
func (c *consoleSessions) abort(vmID string, session, _ *consoleSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session.done = true
	if c.current[vmID] != session {
		return // a newer session has already taken over; its own end or abort settles the entry
	}
	alive := session.previous
	for alive != nil && alive.done {
		alive = alive.previous
	}
	if alive == nil {
		delete(c.current, vmID)
		return
	}
	alive.replaced.Store(false)
	c.current[vmID] = alive
}

// end removes a finished session unless a newer one has already taken its place.
func (c *consoleSessions) end(vmID string, session *consoleSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session.done = true
	if c.current[vmID] == session {
		delete(c.current, vmID)
	}
}

func (c *consoleSessions) inUse(vmID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current[vmID] != nil
}
