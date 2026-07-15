package main

import (
	"crypto/rand"
	"errors"
	"math/big"
	"net/mail"
	"strings"
	"unicode"
)

// Validation rules mirror open-oscar-server's state package so a signup that
// passes here cannot be rejected later by the management API.

var (
	errScreenNameLength = errors.New("screen name must have 3-16 characters (at least 3 letters or digits)")
	errScreenNameFormat = errors.New("screen name must start with a letter, contain only letters, digits, and spaces, and not end with a space")
	errPasswordLength   = errors.New("password must be 4-16 characters")
	errEmailInvalid     = errors.New("that doesn't look like a valid email address")
)

// validateScreenName mirrors DisplayScreenName.ValidateAIMHandle.
func validateScreenName(s string) error {
	if len(s) == 0 || len(s) > 16 {
		return errScreenNameLength
	}

	c := 0
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			c++
		case r != ' ':
			return errScreenNameFormat
		}
	}
	if c < 3 {
		return errScreenNameLength
	}

	if !unicode.IsLetter(rune(s[0])) || s[len(s)-1] == ' ' {
		return errScreenNameFormat
	}

	return nil
}

// validatePassword mirrors validateAIMPassword (AOL's circa-2000 rules).
func validatePassword(pass string) error {
	if len(pass) < 4 || len(pass) > 16 {
		return errPasswordLength
	}
	return nil
}

// passwordAlphabet skips lookalike characters (0/O, 1/l/I) since users
// transcribe the generated password from the success page by hand.
const passwordAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"

// generatePassword returns a random one-time password within AIM's
// 4-16 character limit.
func generatePassword() (string, error) {
	buf := make([]byte, 12)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range buf {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buf[i] = passwordAlphabet[n.Int64()]
	}
	return string(buf), nil
}

// validateEmail applies the same RFC 5322 + length checks as the server's
// Admin food group.
func validateEmail(address string) (string, error) {
	e, err := mail.ParseAddress(address)
	if err != nil {
		return "", errEmailInvalid
	}
	if len(e.Address) > 320 {
		return "", errEmailInvalid
	}
	return e.Address, nil
}

// identScreenName mirrors NewIdentScreenName: the canonical form used for
// uniqueness — lowercase with spaces removed.
func identScreenName(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", ""))
}
