package mock

import (
	"context"
	"testing"
)

func TestPauseContextCompletionAndCancellation(t *testing.T) {
	completed, err := pauseContext(context.Background(), 0)
	if err != nil || !completed {
		t.Fatalf("zero pause = %v, %v; want complete", completed, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	completed, err = pauseContext(ctx, 3600000)
	if err != nil || completed {
		t.Fatalf("cancelled pause = %v, %v; want cancelled", completed, err)
	}
	if _, err = pauseContext(context.Background(), -1); err == nil {
		t.Fatal("negative pause accepted")
	}
}

func TestMCPResponseQueuePreservesEventOrder(t *testing.T) {
	state := &store{values: map[string]string{}}
	x := &exchange{state: state, responseQueue: "session"}
	enqueueResponse(x, "Jfirst")
	enqueueResponse(x, "Esecond")
	if got := dequeueResponse(state, "session"); got != "Jfirst" {
		t.Fatalf("first queued event = %q", got)
	}
	if got := dequeueResponse(state, "session"); got != "Esecond" {
		t.Fatalf("second queued event = %q", got)
	}
	if got := dequeueResponse(state, "session"); got != "" {
		t.Fatalf("queue not empty: %q", got)
	}
}
