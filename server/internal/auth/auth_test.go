package auth

import "testing"

func TestHashIsSHA256(t *testing.T) {
	h := HashToken("secret", "")
	if len(h) != 64 {
		t.Fatalf("expected 64-hex sha256, got len %d", len(h))
	}
	// Deterministic.
	if HashToken("secret", "") != h {
		t.Fatal("hash not deterministic")
	}
	// Different input -> different output.
	if HashToken("other", "") == h {
		t.Fatal("collision")
	}
}

func TestHashPepperChangesOutput(t *testing.T) {
	plain := HashToken("secret", "")
	withPepper := HashToken("secret", "pepper")
	if plain == withPepper {
		t.Fatal("pepper did not change the digest")
	}
	if len(withPepper) != 64 {
		t.Fatalf("pepper hash wrong length: %d", len(withPepper))
	}
}

func TestVerifyAndConstantTime(t *testing.T) {
	stored := HashToken("hunter2", "pepper")
	if !Verify("hunter2", stored, "pepper") {
		t.Fatal("verify should accept correct token")
	}
	if Verify("wrong", stored, "pepper") {
		t.Fatal("verify should reject wrong token")
	}
	// Empty raw never verifies against a real hash.
	if Verify("", stored, "pepper") {
		t.Fatal("empty token must not verify")
	}
}

func TestEqualHashLengthMismatch(t *testing.T) {
	if EqualHash("abc", "abcd") {
		t.Fatal("length mismatch should be false")
	}
	if EqualHash("", "") {
		t.Fatal("empty strings should be false")
	}
}

func TestCheckerAuthenticate(t *testing.T) {
	tokens := []WebhookToken{
		{TokenID: "bootstrap", Hash: HashToken("real-token", "p"), Enabled: true, Revoked: false},
	}
	c := NewChecker(tokens, "p")
	got, err := c.Authenticate("real-token")
	if err != nil || got.TokenID != "bootstrap" {
		t.Fatalf("expected bootstrap, got %v %v", got, err)
	}
	if _, err := c.Authenticate("nope"); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if _, err := c.Authenticate(""); err != ErrUnauthorized {
		t.Fatal("empty token must be unauthorized")
	}
}

func TestCheckerRejectsDisabledOrRevoked(t *testing.T) {
	tokens := []WebhookToken{
		{TokenID: "disabled", Hash: HashToken("t", ""), Enabled: false, Revoked: false},
		{TokenID: "revoked", Hash: HashToken("t", ""), Enabled: true, Revoked: true},
	}
	c := NewChecker(tokens, "")
	if _, err := c.Authenticate("t"); err != ErrUnauthorized {
		t.Fatal("disabled/revoked token must not authenticate")
	}
}
