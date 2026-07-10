package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustReq(t *testing.T, j string) *NotificationRequest {
	t.Helper()
	var req NotificationRequest
	if err := json.Unmarshal([]byte(j), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &req
}

func validJSON() string {
	return `{"receiver_id":"phone-main","title":"订单已支付","body":"订单 123 已完成支付","priority":"normal","ttl_seconds":3600,"group_key":"orders","icon":"default","data":{"order_id":"123"}}`
}

func TestValidateOK(t *testing.T) {
	v, err := Validate(mustReq(t, validJSON()))
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if v.ReceiverID != "phone-main" || v.Priority != "normal" || v.TTLSeconds != 3600 || v.GroupKey != "orders" || v.Icon != "default" {
		t.Fatalf("unexpected normalized value: %+v", v)
	}
	if string(v.DataJSON) != `{"order_id":"123"}` {
		t.Fatalf("data not canonicalized: %s", v.DataJSON)
	}
}

func TestValidateDefaults(t *testing.T) {
	v, err := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b"}`))
	if err != nil {
		t.Fatal(err)
	}
	if v.Priority != "normal" || v.TTLSeconds != 3600 || v.Icon != "default" {
		t.Fatalf("defaults wrong: %+v", v)
	}
	if string(v.DataJSON) != "{}" {
		t.Fatalf("absent data should default to {}, got %s", v.DataJSON)
	}
}

func TestValidateReceiverID(t *testing.T) {
	cases := map[string]string{
		"missing":  `{"title":"t","body":"b"}`,
		"empty":    `{"receiver_id":"","title":"t","body":"b"}`,
		"array":    `{"receiver_id":["a"],"title":"t","body":"b"}`,
		"object":   `{"receiver_id":{"x":1},"title":"t","body":"b"}`,
		"number":   `{"receiver_id":5,"title":"t","body":"b"}`,
		"null":     `{"receiver_id":null,"title":"t","body":"b"}`,
	}
	for name, j := range cases {
		if _, err := Validate(mustReq(t, j)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestValidateTitleBody(t *testing.T) {
	long := strings.Repeat("x", 81)
	big := strings.Repeat("y", 501)
	cases := map[string]string{
		"missing_title": `{"receiver_id":"r","body":"b"}`,
		"empty_title":   `{"receiver_id":"r","title":"","body":"b"}`,
		"title_long":    `{"receiver_id":"r","title":"` + long + `","body":"b"}`,
		"missing_body":  `{"receiver_id":"r","title":"t"}`,
		"empty_body":    `{"receiver_id":"r","title":"t","body":""}`,
		"body_long":     `{"receiver_id":"r","title":"t","body":"` + big + `"}`,
	}
	for name, j := range cases {
		if _, err := Validate(mustReq(t, j)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestValidatePriorityEnum(t *testing.T) {
	for _, p := range []string{"low", "normal", "high"} {
		v, err := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b","priority":"`+p+`"}`))
		if err != nil || v.Priority != p {
			t.Fatalf("priority %s rejected: %v", p, err)
		}
	}
	if _, err := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b","priority":"urgent"}`)); err == nil {
		t.Fatal("bad priority accepted")
	}
}

func TestValidateTTLRange(t *testing.T) {
	for _, ttl := range []int{59, 0, -1, 86401, 100000} {
		j := `{"receiver_id":"r","title":"t","body":"b","ttl_seconds":` + itoa(ttl) + `}`
		if _, err := Validate(mustReq(t, j)); err == nil {
			t.Errorf("ttl %d should be rejected", ttl)
		}
	}
	for _, ttl := range []int{60, 3600, 86400} {
		j := `{"receiver_id":"r","title":"t","body":"b","ttl_seconds":` + itoa(ttl) + `}`
		if _, err := Validate(mustReq(t, j)); err != nil {
			t.Errorf("ttl %d should be accepted: %v", ttl, err)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func TestValidateGroupKeyAndIcon(t *testing.T) {
	gk := strings.Repeat("g", 65)
	if _, err := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b","group_key":"`+gk+`"}`)); err == nil {
		t.Fatal("long group_key accepted")
	}
	if _, err := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b","icon":"custom"}`)); err == nil {
		t.Fatal("non-default icon accepted")
	}
}

func TestValidateData(t *testing.T) {
	if _, err := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b","data":[1,2]}`)); err == nil {
		t.Fatal("array data accepted")
	}
	if _, err := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b","data":"str"}`)); err == nil {
		t.Fatal("string data accepted")
	}
	big := `{"k":"` + strings.Repeat("z", MaxDataBytes) + `"}`
	if _, err := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b","data":`+big+`}`)); err == nil {
		t.Fatal("oversized data accepted")
	}
}

func TestContentHashStability(t *testing.T) {
	base, _ := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b","priority":"normal","data":{"a":1,"b":2}}`))
	// Same content, different key order in data -> same hash.
	perm, _ := Validate(mustReq(t, `{"receiver_id":"r","title":"t","body":"b","priority":"normal","data":{"b":2,"a":1}}`))
	if ContentHash(base) != ContentHash(perm) {
		t.Fatal("data key order affected content hash")
	}

	// ttl / idempotency_key / receiver_id excluded from hash.
	otherTTL, _ := Validate(mustReq(t, `{"receiver_id":"r2","title":"t","body":"b","priority":"normal","idempotency_key":"k","ttl_seconds":120,"data":{"a":1,"b":2}}`))
	if ContentHash(base) != ContentHash(otherTTL) {
		t.Fatal("ttl/idempotency/receiver should not affect content hash")
	}

	// Different title -> different hash.
	diffTitle, _ := Validate(mustReq(t, `{"receiver_id":"r","title":"T2","body":"b","priority":"normal","data":{"a":1,"b":2}}`))
	if ContentHash(base) == ContentHash(diffTitle) {
		t.Fatal("different title produced same hash")
	}
}
