package integration

import "testing"

func TestStdioPingReturnsEmptyResult(t *testing.T) {
	r := oneCall(t, newIsolatedHome(t), "ping", "{}")
	if r.Error != nil {
		t.Fatalf("ping error: %v", r.Error)
	}
	if string(r.Result) != "{}" {
		t.Errorf("ping should return empty result object, got %s", r.Result)
	}
}
