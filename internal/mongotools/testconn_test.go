package mongotools

import (
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// TestTestConnectionBoundedEvenWhenConnectStalls is the regression test
// for #77: TestConnection called options.Client().ApplyURI(uri) — which,
// for a mongodb+srv:// URI, performs synchronous DNS SRV/TXT resolution
// via the OS resolver — before ctx/cancel were used at all. The 10-second
// timeout only ever guarded the Ping/ListDatabaseNames calls that came
// after ApplyURI had already returned, so a slow or unreachable DNS
// resolver could block the whole connection test well past its intended
// budget.
//
// Real slow/hanging DNS isn't practical to reproduce hermetically (a
// bogus hostname typically resolves to NXDOMAIN quickly in most
// environments, this one included), so connectFunc — the seam standing
// in for "the potentially DNS-bound work of building a client" — is
// substituted with a deliberately slow implementation instead. This
// proves the same thing a slow real resolver would: the fix must bound
// the whole operation, not just what comes after it.
func TestTestConnectionBoundedEvenWhenConnectStalls(t *testing.T) {
	origTimeout := testConnectionTimeout
	testConnectionTimeout = 100 * time.Millisecond
	defer func() { testConnectionTimeout = origTimeout }()

	origConnect := connectFunc
	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })
	connectFunc = func(uri string) (*mongo.Client, error) {
		<-unblock // simulates a DNS resolution that never returns in time
		return nil, errors.New("should never be reached within the test")
	}
	defer func() { connectFunc = origConnect }()

	start := time.Now()
	_, err := TestConnection("mongodb+srv://user:pass@doesnotmatter.example/db")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from a connection test whose connect step never returns in time")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("TestConnection took %s to return; expected it to return promptly after the shrunk %s timeout instead of waiting on the stalled connect step", elapsed, testConnectionTimeout)
	}
}
