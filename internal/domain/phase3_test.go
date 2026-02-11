package domain

import "testing"

// ═══════════════════════════════════════════════════════════════════════════
// Region Tests — Phase 3
// ═══════════════════════════════════════════════════════════════════════════

func TestRegionID_String(t *testing.T) {
	tests := []struct {
		region RegionID
		want   string
	}{
		{RegionUSEast, "us-east"},
		{RegionEUWest, "eu-west"},
		{RegionAPSouth, "ap-south"},
	}
	for _, tt := range tests {
		t.Run(string(tt.region), func(t *testing.T) {
			if got := tt.region.String(); got != tt.want {
				t.Errorf("RegionID.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegionID_IsValid(t *testing.T) {
	tests := []struct {
		region RegionID
		valid  bool
	}{
		{RegionUSEast, true},
		{RegionEUWest, true},
		{RegionAPSouth, true},
		{RegionID("us-west"), false},
		{RegionID(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.region), func(t *testing.T) {
			if got := tt.region.IsValid(); got != tt.valid {
				t.Errorf("RegionID(%q).IsValid() = %v, want %v", tt.region, got, tt.valid)
			}
		})
	}
}

func TestAllRegions(t *testing.T) {
	regions := AllRegions()
	if len(regions) != 3 {
		t.Fatalf("AllRegions() returned %d regions, want 3", len(regions))
	}
	seen := make(map[RegionID]bool)
	for _, r := range regions {
		if !r.IsValid() {
			t.Errorf("AllRegions() contains invalid region %q", r)
		}
		if seen[r] {
			t.Errorf("AllRegions() contains duplicate region %q", r)
		}
		seen[r] = true
	}
}

func TestRegionLatencyMs_SameRegion(t *testing.T) {
	for _, r := range AllRegions() {
		lat := RegionLatencyMs(r, r)
		if lat != 0 {
			t.Errorf("RegionLatencyMs(%s, %s) = %d, want 0", r, r, lat)
		}
	}
}

func TestRegionLatencyMs_CrossRegion(t *testing.T) {
	tests := []struct {
		from, to RegionID
		wantMin  int
	}{
		{RegionUSEast, RegionEUWest, 50},
		{RegionUSEast, RegionAPSouth, 100},
		{RegionEUWest, RegionAPSouth, 80},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"-"+string(tt.to), func(t *testing.T) {
			lat := RegionLatencyMs(tt.from, tt.to)
			if lat < tt.wantMin {
				t.Errorf("RegionLatencyMs(%s, %s) = %d, want >= %d", tt.from, tt.to, lat, tt.wantMin)
			}
			// Verify symmetry: (a,b) == (b,a)
			reverse := RegionLatencyMs(tt.to, tt.from)
			if lat != reverse {
				t.Errorf("asymmetric latency: (%s→%s)=%d != (%s→%s)=%d", tt.from, tt.to, lat, tt.to, tt.from, reverse)
			}
		})
	}
}

func TestRegionLatencyMs_UnknownPair(t *testing.T) {
	unknown := RegionID("us-west")
	lat := RegionLatencyMs(unknown, RegionUSEast)
	if lat != 200 {
		t.Errorf("RegionLatencyMs(unknown, us-east) = %d, want 200 (default)", lat)
	}
}

func TestRegionStatus_Load(t *testing.T) {
	tests := []struct {
		name       string
		status     RegionStatus
		wantMinLoad float64
		wantMaxLoad float64
	}{
		{"idle", RegionStatus{NodeCount: 10, ActiveTasks: 0}, 0.0, 0.01},
		{"half_loaded", RegionStatus{NodeCount: 10, ActiveTasks: 5}, 0.49, 0.51},
		{"overloaded", RegionStatus{NodeCount: 10, ActiveTasks: 20}, 1.99, 2.01},
		{"no_nodes", RegionStatus{NodeCount: 0, ActiveTasks: 5}, 0.99, 1.01},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			load := tt.status.Load()
			if load < tt.wantMinLoad || load > tt.wantMaxLoad {
				t.Errorf("Load() = %f, want in [%f, %f]", load, tt.wantMinLoad, tt.wantMaxLoad)
			}
		})
	}
}

func TestTaskRouting_PreferredRegion(t *testing.T) {
	tr := TaskRouting{RegionAffinity: []RegionID{RegionEUWest, RegionUSEast}}
	if got := tr.PreferredRegion(); got != RegionEUWest {
		t.Errorf("PreferredRegion() = %q, want %q", got, RegionEUWest)
	}

	empty := TaskRouting{}
	if got := empty.PreferredRegion(); got != "" {
		t.Errorf("PreferredRegion() on empty = %q, want empty", got)
	}
}

func TestTaskRouting_RequiresRegion(t *testing.T) {
	yes := TaskRouting{DataResidency: RegionEUWest}
	if !yes.RequiresRegion() {
		t.Error("RequiresRegion() = false, want true when DataResidency is set")
	}

	no := TaskRouting{}
	if no.RequiresRegion() {
		t.Error("RequiresRegion() = true, want false when DataResidency is empty")
	}
}

func TestPhase3Errors(t *testing.T) {
	errors := []error{
		ErrBackPressureSoft,
		ErrBackPressureMedium,
		ErrBackPressureHard,
		ErrCircuitOpen,
		ErrCircuitHalfOpen,
		ErrNodeQuarantined,
		ErrNATTraversalFailed,
		ErrTURNUnavailable,
	}
	for _, e := range errors {
		if e == nil {
			t.Error("expected non-nil error")
		}
		if e.Error() == "" {
			t.Error("expected non-empty error message")
		}
	}
}
