package hub

import (
	"testing"
	"time"
)

func TestOnlineAndSend(t *testing.T) {
	h := New()
	if h.IsOnline("r") {
		t.Fatal("receiver should start offline")
	}
	ch, replaced, release := h.Open("r")
	defer release()
	if !h.IsOnline("r") {
		t.Fatal("receiver should be online after Open")
	}
	if !h.Send("r", []byte("hello")) {
		t.Fatal("send to online receiver should succeed")
	}
	select {
	case msg := <-ch:
		if string(msg) != "hello" {
			t.Fatalf("unexpected payload %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive payload")
	}
	// Offline receiver cannot receive.
	if h.Send("missing", []byte("x")) {
		t.Fatal("send to offline should fail")
	}
	_ = replaced
}

func TestReplaceSupersedesOld(t *testing.T) {
	h := New()
	_, replaced1, release1 := h.Open("r")
	defer release1()
	select {
	case <-replaced1:
		t.Fatal("first connection should not be replaced yet")
	default:
	}

	// A second connection supersedes the first.
	ch2, replaced2, release2 := h.Open("r")
	defer release2()

	select {
	case <-replaced1:
		// expected: first connection's replaced channel closed
	case <-time.After(time.Second):
		t.Fatal("first connection was not superseded")
	}

	// New payload goes to the second connection.
	if !h.Send("r", []byte("second")) {
		t.Fatal("send to current connection failed")
	}
	select {
	case m := <-ch2:
		if string(m) != "second" {
			t.Fatalf("unexpected payload %q", m)
		}
	case <-time.After(time.Second):
		t.Fatal("second connection did not receive")
	}
	select {
	case <-replaced2:
		t.Fatal("second connection should not be replaced")
	default:
	}
}

func TestReleaseRemovesOnline(t *testing.T) {
	h := New()
	_, _, release := h.Open("r")
	if !h.IsOnline("r") {
		t.Fatal("expected online")
	}
	release()
	if h.IsOnline("r") {
		t.Fatal("expected offline after release")
	}
}

func TestReleaseDoesNotEvictNewer(t *testing.T) {
	h := New()
	_, _, release1 := h.Open("r")
	_, _, release2 := h.Open("r") // supersedes first
	release1() // stale handler releasing; must NOT remove the newer connection
	if !h.IsOnline("r") {
		t.Fatal("newer connection should remain online after stale release")
	}
	release2()
	if h.IsOnline("r") {
		t.Fatal("expected offline after newest release")
	}
}
