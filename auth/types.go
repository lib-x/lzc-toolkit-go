// Package auth implements the LazyCat account token contract used by
// lzc-cli 2.0.9 without interactive prompts.
package auth

import "context"

const (
	DefaultAccountURL = "https://account.lazycat.cloud"
	DefaultTokenEnv   = "LZC_CLI_TOKEN"
)

type Credentials struct {
	Username string
	Password string
}

type Session struct {
	Token string
}

type TokenProvider interface {
	Token(context.Context) (string, error)
}

type TokenStore interface {
	Load(context.Context) (string, error)
	Save(context.Context, string) error
	Delete(context.Context) error
}
