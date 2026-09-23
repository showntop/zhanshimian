package domain

import (
	"context"
	"testing"
)

func TestInvocationScopeRoundTrip(t *testing.T) {
	scope := InvocationScope{UserID: "u1", OperationID: "op1", TaskID: "t1", AttemptNo: 3}
	got, ok := InvocationScopeFrom(WithInvocationScope(context.Background(), scope))
	if !ok || got != scope {
		t.Fatalf("scope round trip failed: %#v ok=%v", got, ok)
	}
	if _, ok := InvocationScopeFrom(context.Background()); ok {
		t.Fatal("empty context must report no scope")
	}
}
