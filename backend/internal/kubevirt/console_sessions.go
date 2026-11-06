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
}

func (s *consoleSession) closeCode() int {
	if s.replaced.Load() {
		return ConsoleReplacedCloseCode
	}
	return consoleCloseNormal
}

// consoleSessions tracks the current console session of each VM. Every console connection goes through this
// API (a single replica), so it knows whether a console is in use.
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
	session = &consoleSession{}
	c.current[vmID] = session
	return session, previous
}

// abort undoes begin when the new session could not connect, so the previous one stays current.
func (c *consoleSessions) abort(vmID string, session, previous *consoleSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current[vmID] != session {
		return
	}
	if previous != nil {
		previous.replaced.Store(false)
		c.current[vmID] = previous
		return
	}
	delete(c.current, vmID)
}

// end removes a finished session unless a newer one has already taken its place.
func (c *consoleSessions) end(vmID string, session *consoleSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current[vmID] == session {
		delete(c.current, vmID)
	}
}

func (c *consoleSessions) inUse(vmID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current[vmID] != nil
}
