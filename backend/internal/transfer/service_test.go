package transfer

import "testing"

func TestAdvanceToDoneAfterVerification(t *testing.T) {
	state := NewStateMachine().Start("hello.txt", 1024)
	state.MarkReceiving()
	state.MarkVerified()
	state.MarkDone()

	if state.State != "done" {
		t.Fatalf("expected done, got %s", state.State)
	}
}
