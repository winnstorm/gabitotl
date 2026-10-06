package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
)

// TokenPrefix starts every token, so a leaked one is recognizable in a scan.
const TokenPrefix = "gbt_"

// Token errors. They never say whether the id or the secret was wrong.
var (
	ErrBadToken  = errors.New("invalid token")
	ErrBadUserID = errors.New("user id must be a lowercase slug (letters, digits and dashes)")
)

// NewToken creates a token for user and stores only its hash. The returned
// plaintext is the one and only time the secret exists outside the client.
//
// Format: gbt_<16 hex id>.<43 base64url chars>. The id is public and selects the
// row; the secret (256 random bits) is compared by its SHA-256 in constant time.
func NewToken(ctx context.Context, ts store.TokenStore, user string, now time.Time) (plain string, rec store.TokenRecord, err error) {
	if err := CheckUserID(user); err != nil {
		return "", rec, err
	}
	var idb [8]byte
	var sec [32]byte
	if _, err := rand.Read(idb[:]); err != nil {
		return "", rec, err
	}
	if _, err := rand.Read(sec[:]); err != nil {
		return "", rec, err
	}
	id := hex.EncodeToString(idb[:])
	secret := base64.RawURLEncoding.EncodeToString(sec[:])
	sum := sha256.Sum256([]byte(secret))
	rec = store.TokenRecord{ID: id, User: user, Hash: sum[:], CreatedAt: now.UTC()}
	if err := ts.CreateToken(ctx, rec); err != nil {
		return "", store.TokenRecord{}, err
	}
	return TokenPrefix + id + "." + secret, rec, nil
}

// CheckUserID validates a token owner. The id becomes the actor of everything
// the token writes, so it must be a canonical slug; "legacy" is reserved for
// imports and can never be an authenticated actor.
func CheckUserID(user string) error {
	if !gabitotl.ValidID(user) {
		return ErrBadUserID
	}
	if user == memory.ActorLegacy {
		return fmt.Errorf("user id %q is reserved for imported history", memory.ActorLegacy)
	}
	return nil
}

func splitToken(tok string) (id, secret string, ok bool) {
	rest, found := strings.CutPrefix(tok, TokenPrefix)
	if !found {
		return "", "", false
	}
	id, secret, found = strings.Cut(rest, ".")
	if !found || len(id) != 16 || len(secret) < 20 || len(secret) > 100 {
		return "", "", false
	}
	return id, secret, true
}

// dummyHash is compared against when the id is unknown, so a miss costs the
// same as a wrong secret.
var dummyHash = sha256.Sum256([]byte("gabitotl-no-such-token"))

// VerifyToken returns the user a token belongs to. Every failure is the same
// ErrBadToken.
func VerifyToken(ctx context.Context, ts store.TokenStore, tok string) (user string, err error) {
	id, secret, ok := splitToken(tok)
	if !ok {
		_ = subtle.ConstantTimeCompare(dummyHash[:], dummyHash[:])
		return "", ErrBadToken
	}
	rec, lerr := ts.TokenByID(ctx, id)
	want := dummyHash[:]
	if lerr == nil {
		want = rec.Hash
	}
	got := sha256.Sum256([]byte(secret))
	match := subtle.ConstantTimeCompare(got[:], want) == 1
	switch {
	case lerr != nil && !errors.Is(lerr, store.ErrNotFound):
		return "", lerr // the store is down: not the client's fault
	case lerr != nil || !match || !rec.Active():
		return "", ErrBadToken
	}
	return rec.User, nil
}
