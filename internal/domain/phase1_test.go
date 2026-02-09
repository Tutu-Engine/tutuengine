package domain

import (
	"testing"
	"time"
)

// ─── Task Tests ─────────────────────────────────────────────────────────────

func TestTaskStatus_Constants(t *testing.T) {
	statuses := []TaskStatus{
		TaskQueued, TaskAssigned, TaskExecuting,
		TaskCompleted, TaskFailed, TaskCancelled,
	}
	seen := make(map[TaskStatus]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate TaskStatus: %s", s)
		}
		seen[s] = true
	}
	if len(seen) != 6 {
		t.Errorf("expected 6 unique TaskStatus, got %d", len(seen))
	}
}

func TestTask_IsTerminal(t *testing.T) {
	tests := []struct {
		status   TaskStatus
		terminal bool
	}{
		{TaskQueued, false},
		{TaskAssigned, false},
		{TaskExecuting, false},
		{TaskCompleted, true},
		{TaskFailed, true},
		{TaskCancelled, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			task := Task{Status: tt.status}
			if got := task.IsTerminal(); got != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}

func TestTask_Duration(t *testing.T) {
	start := time.Now()
	end := start.Add(5 * time.Second)

	task := Task{StartedAt: start, CompletedAt: end}
	if d := task.Duration(); d != 5*time.Second {
		t.Errorf("Duration() = %v, want 5s", d)
	}

	// Not started
	task2 := Task{}
	if d := task2.Duration(); d != 0 {
		t.Errorf("Duration() of unstarted task = %v, want 0", d)
	}
}

// ─── Peer Tests ─────────────────────────────────────────────────────────────

func TestPeerState_Constants(t *testing.T) {
	if PeerAlive != "ALIVE" {
		t.Errorf("PeerAlive = %q, want ALIVE", PeerAlive)
	}
	if PeerSuspect != "SUSPECT" {
		t.Errorf("PeerSuspect = %q, want SUSPECT", PeerSuspect)
	}
	if PeerDead != "DEAD" {
		t.Errorf("PeerDead = %q, want DEAD", PeerDead)
	}
}

func TestPeer_IsReachable(t *testing.T) {
	tests := []struct {
		state     PeerState
		reachable bool
	}{
		{PeerAlive, true},
		{PeerSuspect, false},
		{PeerDead, false},
	}
	for _, tt := range tests {
		peer := Peer{State: tt.state}
		if got := peer.IsReachable(); got != tt.reachable {
			t.Errorf("IsReachable() with state %s = %v, want %v", tt.state, got, tt.reachable)
		}
	}
}

func TestPeer_IsTrusted(t *testing.T) {
	peer := Peer{Reputation: 0.7}
	if !peer.IsTrusted(0.5) {
		t.Error("0.7 should be trusted at 0.5 threshold")
	}
	if peer.IsTrusted(0.8) {
		t.Error("0.7 should NOT be trusted at 0.8 threshold")
	}
}

// ─── Resource Tests ─────────────────────────────────────────────────────────

func TestIdleLevel_String(t *testing.T) {
	tests := []struct {
		level IdleLevel
		want  string
	}{
		{IdleActive, "active"},
		{IdleLight, "light"},
		{IdleDeep, "deep"},
		{IdleLocked, "locked"},
		{IdleServer, "server"},
		{IdleLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("IdleLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestComputeBudget_CanAcceptWork(t *testing.T) {
	tests := []struct {
		name   string
		budget ComputeBudget
		want   bool
	}{
		{"active", ComputeBudget{MaxCPUPercent: 10, AllowDistributed: false}, false},
		{"deep", ComputeBudget{MaxCPUPercent: 80, AllowDistributed: true}, true},
		{"zero cpu", ComputeBudget{MaxCPUPercent: 0, AllowDistributed: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.budget.CanAcceptWork(); got != tt.want {
				t.Errorf("CanAcceptWork() = %v, want %v", got, tt.want)
			}
		})
	}
}
