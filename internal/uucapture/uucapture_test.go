package uucapture

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

const serverPath = "/Applications/UURemote.app/Contents/Helpers/UURemoteServer"

func ts(t *testing.T, clock string) time.Time {
	t.Helper()
	at, err := time.Parse(timeLayout, "2026-11-12 "+clock+"-0800")
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func line(clock, text string, pid int) string {
	entry := map[string]any{
		"timestamp":        "2026-11-12 " + clock + "-0800",
		"processImagePath": serverPath,
		"eventMessage":     text,
	}
	if pid != 0 {
		entry["processID"] = pid
	}
	b, err := json.Marshal(entry)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func scDisplay(id string) string {
	return " [INFO] -[SCDisplay initWithDict:]:196 (0x1) displayID=" + id + " frame=(0,0,1320,2868) width=1320 height=2868"
}

const (
	startCapture = " [INFO] -[SCStream startCaptureWithCompletionHandler:]:939 0x1 streamID=<private>"
	stopCapture  = " [INFO] -[SCStream stopCaptureWithCompletionHandler:]:983 0x1 streamID=<private>"
	frameStop    = " [INFO] _SCStream_UpdateAndHandleFrameStatus:1729 Stopping stream due to stream frameStatus=7"
	encCreate    = "<<<< VT-CS >>>> VTCompressionSessionCreateWithOptions: [0x1:CS/ZDF] new compression session (primary-session): codec hvc1"
	encEnd       = "<<<< VT-CS >>>> VTCompressionSessionInvalidate: [0x1:CS/ZDF] invalidating compression session: encoded 750 dropped 0 duration 19581 [ms]"
	encProbeEnd  = "<<<< VT-CS >>>> VTCompressionSessionInvalidate: [0x1:CS/HQO] invalidating compression session"
)

func mustParse(t *testing.T, s string) Record {
	t.Helper()
	r, ok := Parse([]byte(s))
	if !ok {
		t.Fatalf("Parse rejected %s", s)
	}
	return r
}

func TestParseKinds(t *testing.T) {
	cases := map[string]Kind{
		startCapture: CaptureStarted,
		stopCapture:  CaptureStopped,
		frameStop:    CaptureStopped,
		encCreate:    EncoderCreated,
		encEnd:       EncoderEnded,
		encProbeEnd:  EncoderProbeEnded,
	}
	for text, want := range cases {
		if got := mustParse(t, line("19:01:43.417100", text, 0)).Kind; got != want {
			t.Errorf("%q parsed as %v, want %v", text, got, want)
		}
	}
	r := mustParse(t, line("19:01:43.417100", scDisplay("14"), 18369))
	if r.Kind != DisplayDescribed || r.Display != 14 || r.PID != 18369 {
		t.Errorf("display line parsed as %+v", r)
	}
	if !r.Time.Equal(ts(t, "19:01:43.4171")) {
		t.Errorf("time %v", r.Time)
	}
}

func TestParseIgnores(t *testing.T) {
	other := strings.Replace(line("19:01:43.417100", startCapture, 0), "UURemoteServer", "other", 1)
	for _, s := range []string{
		line("19:01:43.417100", " [ERROR] -[SCStream stopCaptureWithCompletionHandler:]_block_invoke_2:995 error=Error Domain=x Code=-3808", 0),
		other,
		`Filtering the log data using "process == UURemoteServer"`,
		`{"count":28,"finished":1}`,
		line("19:01:43.417100", "CGSDisplayNotifyProc: got notification kCGSDisplayWillReconfigure", 0),
		line("19:01:43.417100", " [INFO] -[SCDisplay initWithDict:]:196 (0x1) frame=(0,0,1,1)", 0),
		"",
	} {
		if r, ok := Parse([]byte(s)); ok {
			t.Errorf("Parse accepted %q as %+v", s, r)
		}
	}
}

func TestPredicateCoversEveryKind(t *testing.T) {
	for _, text := range []string{scDisplay("1"), startCapture, stopCapture, frameStop, encCreate, encProbeEnd} {
		if _, _, ok := classify(text); !ok {
			t.Errorf("classify rejects %q", text)
		}
		covered := false
		for _, k := range Keywords {
			covered = covered || strings.Contains(text, k)
		}
		if !covered {
			t.Errorf("no keyword covers %q", text)
		}
	}
	if p := Predicate(); !strings.HasPrefix(p, `process == "UURemoteServer" AND (`) {
		t.Errorf("predicate %s", p)
	}
}

type step struct {
	clock, text string
}

func activity(t *testing.T, steps []step, probes []string) map[string]bool {
	t.Helper()
	var s State
	out := map[string]bool{}
	i := 0
	for _, st := range steps {
		r := mustParse(t, line(st.clock, st.text, 0))
		for i < len(probes) && ts(t, probes[i]).Before(r.Time) {
			out[probes[i]] = s.Active(ts(t, probes[i]))
			i++
		}
		s.Apply(r)
	}
	for ; i < len(probes); i++ {
		out[probes[i]] = s.Active(ts(t, probes[i]))
	}
	return out
}

func TestRestartsDoNotEndASession(t *testing.T) {
	steps := []step{
		{"19:01:02.387000", startCapture},
		{"19:01:23.405000", stopCapture},
		{"19:01:23.527000", encCreate},
		{"19:01:23.561000", startCapture},
		{"19:01:23.822000", encEnd},
		{"19:01:23.831000", encCreate},
		{"19:01:43.417000", stopCapture},
	}
	want := map[string]bool{
		"19:01:02.300": false, "19:01:02.500": true, "19:01:23.700": true, "19:01:27.000": true,
		"19:01:43.000": true, "19:01:46.400": true, "19:01:46.500": false,
	}
	var probes []string
	for p := range want {
		probes = append(probes, p)
	}
	slices.Sort(probes)
	got := activity(t, steps, probes)
	for p, w := range want {
		if got[p] != w {
			t.Errorf("active at %s = %v, want %v", p, got[p], w)
		}
	}
}

func TestGapBetweenSessions(t *testing.T) {
	steps := []step{
		{"20:00:41.831000", startCapture},
		{"20:01:03.774000", stopCapture},
		{"20:01:21.000000", startCapture},
		{"20:01:56.206000", frameStop},
	}
	want := map[string]bool{
		"20:01:06.700": true, "20:01:06.800": false, "20:01:15.000": false,
		"20:01:21.500": true, "20:01:59.100": true, "20:01:59.300": false,
	}
	var probes []string
	for p := range want {
		probes = append(probes, p)
	}
	slices.Sort(probes)
	got := activity(t, steps, probes)
	for p, w := range want {
		if got[p] != w {
			t.Errorf("active at %s = %v, want %v", p, got[p], w)
		}
	}
}

func TestStreamingDisplay(t *testing.T) {
	var s State
	apply := func(clock, text string) { s.Apply(mustParse(t, line(clock, text, 0))) }
	at := func(clock string) uint32 { return s.Streaming(ts(t, clock)) }
	if got := at("20:00:41.000"); got != 0 {
		t.Errorf("before any capture: %d", got)
	}
	apply("20:00:41.830000", scDisplay("1"))
	apply("20:00:41.830100", scDisplay("14"))
	apply("20:00:41.831000", scDisplay("1"))
	apply("20:00:41.831500", startCapture)
	if got := at("20:00:43.000"); got != 1 {
		t.Errorf("first capture shows %d, want 1", got)
	}
	apply("20:00:43.940000", stopCapture)
	apply("20:00:44.046000", scDisplay("14"))
	apply("20:00:44.075500", startCapture)
	apply("20:00:50.000000", scDisplay("1"))
	if got := at("20:01:00.000"); got != 14 {
		t.Errorf("after switching: %d, want 14", got)
	}
	apply("20:01:03.772000", stopCapture)
	if got := at("20:01:06.700"); got != 14 {
		t.Errorf("within the grace: %d, want 14", got)
	}
	if got := at("20:01:06.800"); got != 0 {
		t.Errorf("after the grace: %d, want 0", got)
	}
}

func TestSecondStreamKeepsDisplay(t *testing.T) {
	var s State
	apply := func(clock, text string) { s.Apply(mustParse(t, line(clock, text, 0))) }
	apply("22:57:12.960000", scDisplay("79"))
	apply("22:57:12.969000", startCapture)
	apply("22:57:13.010000", scDisplay("1"))
	apply("22:57:13.023000", startCapture)
	if got := s.Streaming(ts(t, "22:57:14.000")); got != 79 {
		t.Errorf("the second stream moved the display to %d, want 79", got)
	}
	apply("22:57:20.000000", stopCapture)
	apply("22:57:20.040000", scDisplay("5"))
	apply("22:57:20.050000", startCapture)
	if got := s.Streaming(ts(t, "22:57:21.000")); got != 5 {
		t.Errorf("after switching screens: %d, want 5", got)
	}
}

func TestProbesAloneStayInactive(t *testing.T) {
	var s State
	s.Apply(mustParse(t, line("16:50:00.000000", encCreate, 0)))
	s.Apply(mustParse(t, line("16:50:00.000000", encProbeEnd, 0)))
	if s.Active(ts(t, "16:50:01.000")) {
		t.Error("encoder probes made the state active")
	}
}

func rec(t *testing.T, clock, text string, pid int) Record {
	return mustParse(t, line(clock, text, pid))
}

func TestResumed(t *testing.T) {
	history := []Record{
		rec(t, "22:00:00.000000", scDisplay("79"), 100),
		rec(t, "22:00:00.100000", startCapture, 100),
		rec(t, "22:10:00.000000", stopCapture, 100),
	}
	live := []Record{
		rec(t, "22:10:00.000000", stopCapture, 0),
		rec(t, "22:10:00.500000", scDisplay("5"), 0),
		rec(t, "22:10:00.600000", startCapture, 0),
	}
	later := ts(t, "22:11:40.000")
	s := Resumed(history, live, 100)
	if !s.Active(later) || s.Streaming(later) != 5 {
		t.Errorf("resumed: active %v showing %d", s.Active(later), s.Streaming(later))
	}
	if Resumed(nil, live[:1], 100).Active(later) {
		t.Error("a lone stop made it active")
	}
}

func TestResumedIgnoresOtherProcesses(t *testing.T) {
	crashed := []Record{
		rec(t, "22:00:00.000000", scDisplay("79"), 100),
		rec(t, "22:00:00.100000", startCapture, 100),
	}
	later := ts(t, "22:10:00.000")
	if Resumed(crashed, nil, 200).Active(later) {
		t.Error("a crashed server's capture counted")
	}
	if Resumed(crashed, nil, 0).Active(later) {
		t.Error("history counted with no server running")
	}
	if !Resumed(crashed, nil, 100).Active(later) {
		t.Error("the current server's capture did not count")
	}
	current := append(append([]Record(nil), crashed...), rec(t, "22:05:00.000000", startCapture, 200))
	if !Resumed(current, nil, 200).Active(later) {
		t.Error("the new server's own capture did not count")
	}
}
