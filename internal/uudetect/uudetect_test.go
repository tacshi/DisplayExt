package uudetect

import (
	"testing"
	"time"
)

var rule = Rule{
	Process:   "UURemoteServer",
	Assertion: "UURemote Disable Display Sleep",
	Grace:     5 * time.Second,
}

var (
	idleSnap = Snapshot{
		Processes:  map[int32]string{755: "UURemoteServer"},
		Assertions: map[int32][]string{},
	}
	sessionSnap = Snapshot{
		Processes: map[int32]string{755: "UURemoteServer"},
		Assertions: map[int32][]string{
			755: {"UURemote Disable Display Sleep", "Wake Up Display"},
		},
	}

	afterSnap = Snapshot{
		Processes:  map[int32]string{755: "UURemoteServer"},
		Assertions: map[int32][]string{755: {"Wake Up Display"}},
	}
	absentSnap = Snapshot{Processes: map[int32]string{}, Assertions: map[int32][]string{}}
)

func TestSample(t *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
		want Sample
	}{
		{"idle", idleSnap, Sample{Running: true}},
		{"session", sessionSnap, Sample{Running: true, Held: true}},
		{"after disconnect", afterSnap, Sample{Running: true}},
		{"not running", absentSnap, Sample{}},
		{"assertion held by another process", Snapshot{
			Processes:  map[int32]string{755: "UURemoteServer", 900: "Other"},
			Assertions: map[int32][]string{900: {"UURemote Disable Display Sleep"}},
		}, Sample{Running: true}},
	}
	for _, c := range cases {
		if got := rule.Sample(c.snap); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestDetectorGrace(t *testing.T) {
	d := NewDetector(rule)
	t0 := time.Unix(1_000_000, 0)
	at := func(s time.Duration) time.Time { return t0.Add(s) }
	idle, held := Sample{Running: true}, Sample{Running: true, Held: true}

	steps := []struct {
		at      time.Duration
		sample  Sample
		state   State
		changed bool
	}{
		{0, idle, Idle, true},
		{2 * time.Second, idle, Idle, false},
		{4 * time.Second, held, Connected, true},
		{6 * time.Second, held, Connected, false},
		{8 * time.Second, idle, Connected, false},
		{11 * time.Second, held, Connected, false},
		{13 * time.Second, idle, Connected, false},
		{17 * time.Second, idle, Idle, true},
		{19 * time.Second, Sample{}, NotRunning, true},
	}
	for i, s := range steps {
		st, ch := d.Step(at(s.at), s.sample)
		if st != s.state || ch != s.changed {
			t.Fatalf("step %d at %s: got %v/%v, want %v/%v", i, s.at, st, ch, s.state, s.changed)
		}
	}
}

func TestDetectorConnectedFirst(t *testing.T) {
	d := NewDetector(rule)
	if st, ch := d.Step(time.Now(), Sample{Running: true, Held: true}); st != Connected || !ch {
		t.Fatalf("got %v/%v", st, ch)
	}
}

func TestStateString(t *testing.T) {
	for st, want := range map[State]string{NotRunning: "not running", Idle: "not connected", Connected: "connected"} {
		if st.String() != want {
			t.Errorf("%d: %q", st, st.String())
		}
	}
}
