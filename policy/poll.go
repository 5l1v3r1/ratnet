package policy

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/awgh/ratnet"
	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/api/events"
)

// Poll : defines a Polling Connection Policy, which will periodically connect to each remote Peer
type Poll struct {
	// internal
	wg        sync.WaitGroup
	isRunning bool

	// last poll times, 0 as initial value will sync older messages than first connect, any negative value will not
	lastPollLocal, lastPollRemote int64

	Transport api.Transport
	node      api.Node

	Interval    int
	SyncBacklog bool
	Group       string
}

func init() {
	ratnet.Policies["poll"] = NewPollFromMap // register this module by name (for deserialization support)
}

// NewPollFromMap : Makes a new instance of this transport module from a map of arguments (for deserialization support)
func NewPollFromMap(transport api.Transport, node api.Node,
	t map[string]interface{}) api.Policy {
	interval := int(t["Interval"].(float64))
	syncBacklog := bool(t["SyncBacklog"].(bool))
	group := string(t["Group"].(string))
	return NewPoll(transport, node, interval, syncBacklog, group)
}

// NewPoll : Returns a new instance of a Poll Connection Policy
func NewPoll(transport api.Transport, node api.Node, interval int, syncBacklog bool, group ...string) *Poll {
	p := new(Poll)
	// if we don't have a specified group, it's ""
	p.Group = ""
	if len(group) > 0 {
		p.Group = group[0]
	}
	p.Transport = transport
	p.node = node
	p.Interval = interval
	p.SyncBacklog = syncBacklog

	return p
}

// MarshalJSON : Create a serialied representation of the config of this policy
func (p *Poll) MarshalJSON() (b []byte, e error) {
	return json.Marshal(map[string]interface{}{
		"Policy":      "poll",
		"Transport":   p.Transport,
		"Interval":    p.Interval,
		"SyncBacklog": p.SyncBacklog,
		"Group":       p.Group})
}

// RunPolicy : Poll
func (p *Poll) RunPolicy() error {
	if p.isRunning {
		return errors.New("Policy is already running")
	}

	if p.SyncBacklog {
		p.lastPollLocal = 0
		p.lastPollRemote = 0
	} else {
		p.lastPollLocal = -1
		p.lastPollRemote = -1
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if p.isRunning {
			return
		}
		p.isRunning = true

		pubsrv, err := p.node.ID()
		if err != nil {
			events.Critical(p.node, "Couldn't get routing key in Poll.RunPolicy:\n"+err.Error())
		}
		counter := 0
		for {
			// check if we should still be running
			if !p.isRunning {
				break
			}
			time.Sleep(time.Duration(p.Interval) * time.Millisecond) // update interval

			// Get Server List for this Poll's assigned Group
			peers, err := p.node.GetPeers(p.Group)
			if err != nil {
				events.Warning(p.node, "Poll.RunPolicy error in loop: ", err)
				continue
			}
			for _, element := range peers {
				if element.Enabled {
					_, err := PollServer(p.Transport, p.node, element.URI, pubsrv)
					if err != nil {
						events.Warning(p.node, "pollServer error: ", err.Error())
					}
				}
			}

			if counter%500 == 0 {
				p.node.FlushOutbox(300) // seconds to cache
			}
			counter++
		}
	}()

	return nil
}

// Stop : Stops this instance of Poll from running
func (p *Poll) Stop() {
	p.isRunning = false
	p.wg.Wait()
	p.Transport.Stop()
}

// GetTransport : Returns the transports associated with this policy
//
func (p *Poll) GetTransport() api.Transport {
	return p.Transport
}
