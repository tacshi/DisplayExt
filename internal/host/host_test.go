package host

import (
	"testing"
	"time"
)

func TestKeeperPutsBackAFewTimesOnly(t *testing.T) {
	var k keeper
	start := time.Now()
	for i := range holdTimes {
		if !k.allow(start.Add(time.Duration(i) * time.Second)) {
			t.Fatalf("fix %d refused", i+1)
		}
	}
	if k.allow(start.Add(5 * time.Second)) {
		t.Error("a fourth fix within the window was allowed")
	}
	if !k.allow(start.Add(holdWindow + time.Second)) {
		t.Error("no fix allowed once the window passed")
	}
}
