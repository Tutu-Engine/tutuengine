// Package domain — Phase 1 peer types.
// A Peer is a node in the TuTu network discovered via SWIM gossip.
package domain

import "time"

// PeerState tracks SWIM gossip membership state.
type PeerState string

const (
	PeerAlive   PeerState = "ALIVE"
	PeerSuspect PeerState = "SUSPECT"
	PeerDead    PeerState = "DEAD"
)

// Peer represents a known node in the TuTu network.
type Peer struct {
	NodeID     string    `json:"node_id"`
	Region     string    `json:"region"`
	Endpoint   string    `json:"endpoint,omitempty"`
	LastSeen   time.Time `json:"last_seen"`
	Reputation float64   `json:"reputation"`
	State      PeerState `json:"state"`
}

// IsReachable returns true if the peer is alive (not dead or suspect).
func (p *Peer) IsReachable() bool {
	return p.State == PeerAlive
}

// IsTrusted returns true if the peer has reputation above threshold.
func (p *Peer) IsTrusted(threshold float64) bool {
	return p.Reputation >= threshold
}
