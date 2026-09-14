package oauth

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func testKeys(t *testing.T) Keys {
	t.Helper()
	return DeriveKeys(GenerateMasterKey())
}

func TestSealOpenRoundTrip(t *testing.T) {
	keys := testKeys(t)

	type payload struct {
		Value string `json:"value"`
	}

	token, err := seal(keys, "test-purpose", payload{Value: "hello"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	var got payload
	if err := open(keys, "test-purpose", token, &got); err != nil {
		t.Fatalf("open: %v", err)
	}
	if got.Value != "hello" {
		t.Fatalf("got %+v, want Value=hello", got)
	}
}

func TestOpenRejectsTamperedToken(t *testing.T) {
	keys := testKeys(t)

	token, err := seal(keys, "test-purpose", map[string]string{"value": "hello"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	tampered := []rune(token)
	// Flip a character in the middle of the payload rather than the edges,
	// which base64.RawURLEncoding may tolerate as still-valid encoding.
	mid := len(tampered) / 2
	if tampered[mid] == 'A' {
		tampered[mid] = 'B'
	} else {
		tampered[mid] = 'A'
	}

	var got map[string]string
	if err := open(keys, "test-purpose", string(tampered), &got); err != ErrInvalidToken {
		t.Fatalf("open(tampered) = %v, want ErrInvalidToken", err)
	}
}

func TestOpenRejectsWrongPurpose(t *testing.T) {
	keys := testKeys(t)

	token, err := seal(keys, "purpose-a", map[string]string{"value": "hello"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	var got map[string]string
	if err := open(keys, "purpose-b", token, &got); err != ErrInvalidToken {
		t.Fatalf("open(wrong purpose) = %v, want ErrInvalidToken", err)
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	sealedUnder := testKeys(t)
	openedUnder := testKeys(t)

	token, err := seal(sealedUnder, "test-purpose", map[string]string{"value": "hello"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	var got map[string]string
	if err := open(openedUnder, "test-purpose", token, &got); err != ErrInvalidToken {
		t.Fatalf("open(wrong key) = %v, want ErrInvalidToken", err)
	}
}

func TestOpenRejectsGarbage(t *testing.T) {
	keys := testKeys(t)

	var got map[string]string
	if err := open(keys, "test-purpose", "not a valid token", &got); err != ErrInvalidToken {
		t.Fatalf("open(garbage) = %v, want ErrInvalidToken", err)
	}
}

func TestCiphertextDoesNotContainPlaintext(t *testing.T) {
	keys := testKeys(t)

	const secret = "1-supersecretapikey"
	token, err := seal(keys, "test-purpose", map[string]string{"api_key": secret})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// Check the decoded bytes, not the base64 string: base64 regroups the
	// payload into 4-character blocks on 3-byte boundaries, so a plaintext
	// substring's presence or absence in the *encoded* string is mostly an
	// accident of alignment either way and wouldn't reliably catch a
	// regression to "marshal and base64-encode with no actual encryption".
	// The decoded bytes are what a broken seal would expose as literal
	// plaintext JSON.
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decoding sealed token: %v", err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("sealed token leaks the plaintext secret")
	}
}
