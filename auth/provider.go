package auth

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

type StaticToken string

func (provider StaticToken) Token(ctx context.Context) (string, error) {
	if err := authContextError(ctx, "auth.static_token"); err != nil {
		return "", err
	}
	return requireToken(string(provider), "auth.static_token")
}

type EnvironmentToken struct {
	Name   string
	Lookup func(string) (string, bool)
}

func (provider EnvironmentToken) Token(ctx context.Context) (string, error) {
	if err := authContextError(ctx, "auth.environment_token"); err != nil {
		return "", err
	}
	name := strings.TrimSpace(provider.Name)
	if name == "" {
		name = DefaultTokenEnv
	}
	lookup := provider.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, _ := lookup(name)
	return requireToken(value, "auth.environment_token")
}

type StoreProvider struct{ Store TokenStore }

func (provider StoreProvider) Token(ctx context.Context) (string, error) {
	if provider.Store == nil {
		return "", authError(lpkgo.CodeInvalidArgument, "auth.store_provider", errors.New("nil token store"))
	}
	token, err := provider.Store.Load(ctx)
	if err != nil {
		return "", err
	}
	return requireToken(token, "auth.store_provider")
}

type Chain []TokenProvider

func (chain Chain) Token(ctx context.Context) (string, error) {
	for _, provider := range chain {
		if provider == nil {
			continue
		}
		token, err := provider.Token(ctx)
		if err == nil {
			return token, nil
		}
		if !errors.Is(err, lpkgo.ErrUnauthenticated) {
			return "", err
		}
	}
	return "", authError(lpkgo.CodeUnauthenticated, "auth.chain", errors.New("token is unavailable"))
}

type MemoryStore struct {
	mu    sync.RWMutex
	token string
}

func NewMemoryStore(token string) *MemoryStore { return &MemoryStore{token: strings.TrimSpace(token)} }

func (store *MemoryStore) Load(ctx context.Context) (string, error) {
	if err := authContextError(ctx, "auth.memory_store.load"); err != nil {
		return "", err
	}
	if store == nil {
		return "", authError(lpkgo.CodeInvalidArgument, "auth.memory_store.load", errors.New("nil store"))
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.token, nil
}

func (store *MemoryStore) Save(ctx context.Context, token string) error {
	if err := authContextError(ctx, "auth.memory_store.save"); err != nil {
		return err
	}
	normalized, err := requireToken(token, "auth.memory_store.save")
	if err != nil {
		return err
	}
	if store == nil {
		return authError(lpkgo.CodeInvalidArgument, "auth.memory_store.save", errors.New("nil store"))
	}
	store.mu.Lock()
	store.token = normalized
	store.mu.Unlock()
	return nil
}

func (store *MemoryStore) Delete(ctx context.Context) error {
	if err := authContextError(ctx, "auth.memory_store.delete"); err != nil {
		return err
	}
	if store == nil {
		return authError(lpkgo.CodeInvalidArgument, "auth.memory_store.delete", errors.New("nil store"))
	}
	store.mu.Lock()
	store.token = ""
	store.mu.Unlock()
	return nil
}

func requireToken(token, op string) (string, error) {
	normalized := strings.TrimSpace(token)
	if normalized == "" {
		return "", authError(lpkgo.CodeUnauthenticated, op, errors.New("token is empty"))
	}
	return normalized, nil
}

func authContextError(ctx context.Context, op string) error {
	if ctx == nil {
		return authError(lpkgo.CodeInvalidArgument, op, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return authError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

func authError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
