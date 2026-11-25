package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestDPAPIRoundTrip(t *testing.T) {
	secret := "sk-pulse-windows-test-123456"
	enc, err := ProtectDPAPI([]byte(secret))
	if err != nil {
		t.Fatalf("ProtectDPAPI failed: %v", err)
	}
	plain, err := UnprotectDPAPI(enc)
	if err != nil {
		t.Fatalf("UnprotectDPAPI failed: %v", err)
	}
	if string(plain) != secret {
		t.Fatalf("got %q, want %q", string(plain), secret)
	}
}

func TestDecryptChromiumV10AndBuildCursorCookie(t *testing.T) {
	claims, _ := json.Marshal(map[string]any{
		"sub": "auth0|user_win_01",
		"exp": float64(time.Now().Add(2 * time.Hour).Unix()),
	})
	jwt := "eyJhbGciOiJIUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"

	key := []byte("0123456789abcdef0123456789abcdef") // 32 bytes AES-256
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("123456789012")
	sealed := append([]byte("v10"), nonce...)
	sealed = append(sealed, gcm.Seal(nil, nonce, []byte(jwt), nil)...)

	plain, err := decryptChromiumV10(sealed, key)
	if err != nil {
		t.Fatalf("decryptChromiumV10 failed: %v", err)
	}
	if plain != jwt {
		t.Fatalf("got %q, want %q", plain, jwt)
	}

	cookie := buildCursorCookieFromJWT(plain)
	want := fmt.Sprintf("WorkosCursorSessionToken=user_win_01%%3A%%3A%s", jwt)
	if cookie != want {
		t.Fatalf("cookie mismatch:\ngot:  %s\nwant: %s", cookie, want)
	}
}
