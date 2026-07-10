package auth_test

import (
	"context"
	"errors"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestChainUsesFirstAvailableToken(t *testing.T) {
	chain := auth.Chain{
		auth.EnvironmentToken{Lookup: func(string) (string, bool) { return "", false }},
		auth.StaticToken(" direct-token "),
		auth.StaticToken("unused"),
	}
	token, err := chain.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "direct-token" {
		t.Fatalf("token = %q", token)
	}
}

func TestChainReturnsUnauthenticatedWhenEmpty(t *testing.T) {
	_, err := (auth.Chain{auth.StaticToken("")}).Token(context.Background())
	if !errors.Is(err, lpkgo.ErrUnauthenticated) {
		t.Fatalf("error = %v", err)
	}
}
