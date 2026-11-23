// Package editor talks to a document editor server (ONLYOFFICE Document Server). It knows nothing about
// storage: the gateway reads and writes files through the SDK and uses this package to sign what the editor
// needs, to check what the editor sends back, and to reach the editor safely.
package editor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var b64 = base64.RawURLEncoding

// Sign returns an HS256 JWT over claims. The shared secret never leaves the gateway.
func Sign(secret string, claims map[string]any) (string, error) {
	head, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	in := b64.EncodeToString(head) + "." + b64.EncodeToString(body)
	return in + "." + b64.EncodeToString(mac(secret, in)), nil
}

func mac(secret, in string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(in))
	return m.Sum(nil)
}

// ErrToken is returned for every token that does not verify.
var ErrToken = errors.New("editor: invalid token")

// Verify checks an HS256 JWT signed with secret and returns its claims. Only HS256 is accepted (never "none"),
// and an "exp" claim, when present, must lie in the future.
func Verify(secret, token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if secret == "" || len(parts) != 3 {
		return nil, ErrToken
	}
	var head struct {
		Alg string `json:"alg"`
	}
	hb, err := b64.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &head) != nil || head.Alg != "HS256" {
		return nil, ErrToken
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac(secret, parts[0]+"."+parts[1])) {
		return nil, ErrToken
	}
	bb, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, ErrToken
	}
	claims := map[string]any{}
	if json.Unmarshal(bb, &claims) != nil {
		return nil, ErrToken
	}
	if exp, ok := claims["exp"].(float64); ok && time.Now().Unix() > int64(exp) {
		return nil, ErrToken
	}
	return claims, nil
}
