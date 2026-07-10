// Package debugbridge implements the LazyCat Developer Tools DebugBridge
// protocol used by lzc-cli 2.0.8.
package debugbridge

import (
	"context"
	"errors"

	lpkgo "github.com/lib-x/lpk-go"
	"github.com/lib-x/lpk-go/remote"
)

type CommandFactory func(tty bool, args ...string) remote.Command
type HostCommandFactory func(tty bool, name string, args ...string) remote.Command

type Option func(*Client)

func WithUID(uid string) Option {
	return func(client *Client) { client.uid = uid }
}

func WithHostCommand(factory HostCommandFactory) Option {
	return func(client *Client) { client.hostCommand = factory }
}

type Client struct {
	runner      remote.Runner
	command     CommandFactory
	hostCommand HostCommandFactory
	uid         string
}

func New(runner remote.Runner, command CommandFactory, options ...Option) *Client {
	client := &Client{runner: runner, command: command}
	for _, option := range options {
		if option != nil {
			option(client)
		}
	}
	return client
}

func (client *Client) Info(ctx context.Context) (remote.BackendInfo, error) {
	version, err := client.Version(ctx)
	if err != nil {
		return remote.BackendInfo{}, err
	}
	platform, err := client.Platform(ctx)
	if err != nil {
		return remote.BackendInfo{}, err
	}
	return remote.BackendInfo{Version: version, Platform: platform}, nil
}

func bridgeError(code lpkgo.Code, op string, cause error) error {
	if cause == nil {
		cause = errors.New("DebugBridge operation failed")
	}
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}

func (client *Client) validate(ctx context.Context, op string) error {
	if ctx == nil || client == nil || client.runner == nil || client.command == nil {
		return bridgeError(lpkgo.CodeInvalidArgument, op, errors.New("nil context, client, runner, or command factory"))
	}
	if err := ctx.Err(); err != nil {
		return bridgeError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}
