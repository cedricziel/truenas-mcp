package oauth

import (
	"testing"
	"time"
)

func TestReplayCacheRejectsSecondClaim(t *testing.T) {
	c := NewReplayCache()
	fakeNow := time.Unix(1700000000, 0)
	c.now = func() time.Time { return fakeNow }

	if !c.Claim("code-1", time.Minute) {
		t.Fatal("first claim should succeed")
	}
	if c.Claim("code-1", time.Minute) {
		t.Fatal("second claim of the same id should fail")
	}
}

func TestReplayCacheAllowsClaimAfterExpiry(t *testing.T) {
	c := NewReplayCache()
	fakeNow := time.Unix(1700000000, 0)
	c.now = func() time.Time { return fakeNow }

	if !c.Claim("code-1", time.Minute) {
		t.Fatal("first claim should succeed")
	}

	fakeNow = fakeNow.Add(2 * time.Minute)
	if !c.Claim("code-1", time.Minute) {
		t.Fatal("claim should succeed again once the earlier claim's ttl has passed")
	}
}

func TestReplayCacheDistinguishesIDs(t *testing.T) {
	c := NewReplayCache()
	if !c.Claim("code-1", time.Minute) {
		t.Fatal("claiming code-1 should succeed")
	}
	if !c.Claim("code-2", time.Minute) {
		t.Fatal("claiming a different id should succeed independently")
	}
}
