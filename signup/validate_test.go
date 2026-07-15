package main

import (
	"errors"
	"testing"
)

func TestValidateScreenName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		err  error
	}{
		{"valid simple", "SmarterChild", nil},
		{"valid with spaces", "xX Punk Gurl Xx", nil},
		{"valid with digits", "veronica99", nil},
		{"minimum letters", "a bc", nil},
		{"too short", "ab", errScreenNameLength},
		{"spaces don't count toward minimum", "a b", errScreenNameLength},
		{"too long", "abcdefghijklmnopq", errScreenNameLength},
		{"empty", "", errScreenNameLength},
		{"starts with digit", "1337kid", errScreenNameFormat},
		{"starts with space", " abc", errScreenNameFormat},
		{"trailing space", "abc ", errScreenNameFormat},
		{"punctuation", "cool_kid", errScreenNameFormat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateScreenName(tc.in); !errors.Is(err, tc.err) {
				t.Errorf("validateScreenName(%q) = %v, want %v", tc.in, err, tc.err)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	if err := validatePassword("abc"); !errors.Is(err, errPasswordLength) {
		t.Errorf("3 chars should fail, got %v", err)
	}
	if err := validatePassword("abcd"); err != nil {
		t.Errorf("4 chars should pass, got %v", err)
	}
	if err := validatePassword("abcdefghijklmnop"); err != nil {
		t.Errorf("16 chars should pass, got %v", err)
	}
	if err := validatePassword("abcdefghijklmnopq"); !errors.Is(err, errPasswordLength) {
		t.Errorf("17 chars should fail, got %v", err)
	}
}

func TestValidateEmail(t *testing.T) {
	if addr, err := validateEmail("Kate <kate@example.com>"); err != nil || addr != "kate@example.com" {
		t.Errorf("display-name form should normalize, got %q, %v", addr, err)
	}
	if _, err := validateEmail("not-an-email"); err == nil {
		t.Error("garbage should fail")
	}
}

func TestIdentScreenName(t *testing.T) {
	if got := identScreenName("xX Punk Gurl Xx"); got != "xxpunkgurlxx" {
		t.Errorf("got %q", got)
	}
}
