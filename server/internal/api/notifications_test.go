package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNotificationsAuthAndReceiverMatrix(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	// 401 missing token (valid content-type).
	resp := postJSON(t, srv, "", "application/json", validBody())
	expectStatus(t, resp, 401)

	// 401 wrong token.
	resp = postJSON(t, srv, "wrong-token", "application/json", validBody())
	expectStatus(t, resp, 401)

	// 415 non-JSON content-type (valid token).
	resp = postJSON(t, srv, testWebhookTok, "text/plain", validBody())
	expectStatus(t, resp, 415)

	// 404 unknown receiver.
	resp = postJSON(t, srv, testWebhookTok, "application/json",
		`{"receiver_id":"ghost","title":"t","body":"b"}`)
	expectStatus(t, resp, 404)

	// 403 disabled receiver.
	resp = postJSON(t, srv, testWebhookTok, "application/json",
		`{"receiver_id":"phone-disabled","title":"t","body":"b"}`)
	expectStatus(t, resp, 403)

	// 403 not-allowlisted receiver.
	resp = postJSON(t, srv, testWebhookTok, "application/json",
		`{"receiver_id":"phone-notlisted","title":"t","body":"b"}`)
	expectStatus(t, resp, 403)

	// 503 receiver online-but-offline (no SSE connection).
	resp = postJSON(t, srv, testWebhookTok, "application/json", validBody())
	expectStatus(t, resp, 503)
}

func TestNotificationsSchema400(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	cases := map[string]string{
		"unknown_field":     `{"receiver_id":"phone-main","title":"t","body":"b","extra":1}`,
		"array_receiver":    `{"receiver_id":["phone-main"],"title":"t","body":"b"}`,
		"bad_priority":      `{"receiver_id":"phone-main","title":"t","body":"b","priority":"urgent"}`,
		"title_too_long":    `{"receiver_id":"phone-main","title":"` + strings.Repeat("x", 81) + `","body":"b"}`,
		"ttl_out_of_range":  `{"receiver_id":"phone-main","title":"t","body":"b","ttl_seconds":10}`,
		"missing_title":     `{"receiver_id":"phone-main","body":"b"}`,
		"icon_not_default":  `{"receiver_id":"phone-main","title":"t","body":"b","icon":"x"}`,
		"data_not_object":   `{"receiver_id":"phone-main","title":"t","body":"b","data":[1]}`,
	}
	for name, body := range cases {
		resp := postJSON(t, srv, testWebhookTok, "application/json", body)
		if resp.StatusCode != 400 {
			t.Errorf("%s: got %d, want 400; body=%s", name, resp.StatusCode, readBody(t, resp))
		} else {
			resp.Body.Close()
		}
	}
}

func TestNotificationsPayloadTooLarge(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)
	big := `{"receiver_id":"phone-main","title":"t","body":"` + strings.Repeat("x", 8200) + `"}`
	resp := postJSON(t, srv, testWebhookTok, "application/json", big)
	expectStatus(t, resp, 413)
}

func TestNotificationsSuccessAndDelivery(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeStream()
	// Drain the initial flush (none expected) and wait for connection to register.
	time.Sleep(80 * time.Millisecond)

	resp := postJSON(t, srv, testWebhookTok, "application/json", validBody())
	expectStatus(t, resp, 201)
	var got struct {
		NotificationID string `json:"notification_id"`
		Status         string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got.Status != "accepted" || !strings.HasPrefix(got.NotificationID, "ntf_") {
		t.Fatalf("unexpected response: %+v", got)
	}

	// The SSE stream should receive exactly one notification event.
	select {
	case f := <-frames:
		if f.event != "notification" {
			t.Fatalf("expected notification event, got %+v", f)
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(f.data), &ev); err != nil {
			t.Fatal(err)
		}
		if ev["type"] != "notification" || ev["title"] != "t" || ev["body"] != bodyCanary {
			t.Fatalf("unexpected event payload: %+v", ev)
		}
		if ev["priority"] != "normal" {
			t.Fatalf("priority mismatch: %v", ev["priority"])
		}
		if _, ok := ev["expires_at"].(string); !ok {
			t.Fatalf("expires_at missing or wrong type: %v", ev["expires_at"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive notification on stream")
	}
}

func TestIdempotencyDuplicateAndConflict(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	defer closeStream()
	time.Sleep(80 * time.Millisecond)

	first := `{"receiver_id":"phone-main","idempotency_key":"k1","title":"A","body":"bA"}`
	second := `{"receiver_id":"phone-main","idempotency_key":"k1","title":"A","body":"bA"}`
	conflict := `{"receiver_id":"phone-main","idempotency_key":"k1","title":"B","body":"bB"}`

	resp := postJSON(t, srv, testWebhookTok, "application/json", first)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	// Duplicate same content -> 200 duplicate.
	resp = postJSON(t, srv, testWebhookTok, "application/json", second)
	expectStatus(t, resp, 200)
	var dup struct {
		NotificationID string `json:"notification_id"`
		Status         string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&dup)
	resp.Body.Close()
	if dup.Status != "duplicate" || !strings.HasPrefix(dup.NotificationID, "ntf_") {
		t.Fatalf("duplicate response wrong: %+v", dup)
	}

	// Same key, different content -> 409.
	resp = postJSON(t, srv, testWebhookTok, "application/json", conflict)
	expectStatus(t, resp, 409)
	resp.Body.Close()

	// Exactly one notification delivered (the first); duplicate/conflict do not.
	delivered := 0
drain:
	for {
		select {
		case <-frames:
			delivered++
		case <-time.After(300 * time.Millisecond):
			break drain
		}
	}
	if delivered != 1 {
		t.Fatalf("expected exactly 1 delivered event, got %d", delivered)
	}
}

func TestDuplicateWorksEvenWhenOffline(t *testing.T) {
	app, _, _ := newTestApp(t, time.Second)
	srv := newServer(t, app)

	// Deliver once while online.
	frames, closeStream := openStream(t, srv, testReceiver, testIdentityTok)
	time.Sleep(80 * time.Millisecond)
	first := `{"receiver_id":"phone-main","idempotency_key":"k2","title":"A","body":"bA"}`
	resp := postJSON(t, srv, testWebhookTok, "application/json", first)
	expectStatus(t, resp, 201)
	resp.Body.Close()
	closeStream()
	// Drain delivered event.
	<-frames

	// Now offline; the same key+content must still return 200 duplicate.
	time.Sleep(80 * time.Millisecond)
	resp = postJSON(t, srv, testWebhookTok, "application/json", first)
	expectStatus(t, resp, 200)
	resp.Body.Close()
}
