package debugbridge

import (
	"context"
	"errors"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/remote"
)

func (client *Client) Docker(ctx context.Context, input remote.StreamRequest) (remote.Result, error) {
	return client.runStream(ctx, "remote.debugbridge.docker", "lzc-docker", input)
}

func (client *Client) DockerCompose(ctx context.Context, input remote.StreamRequest) (remote.Result, error) {
	return client.runStream(ctx, "remote.debugbridge.compose", "lzc-docker-compose", input)
}

func (client *Client) runStream(ctx context.Context, op, commandName string, input remote.StreamRequest) (remote.Result, error) {
	if err := client.validate(ctx, op); err != nil {
		return remote.Result{}, err
	}
	args := make([]string, 0, len(input.Args)+1)
	args = append(args, commandName)
	for _, argument := range input.Args {
		if strings.ContainsAny(argument, "\r\n\x00") {
			return remote.Result{}, bridgeError(lpkgo.CodeInvalidArgument, op, errors.New("invalid command argument"))
		}
		args = append(args, argument)
	}
	command := client.command(input.TTY, args...)
	command.Stdin = input.Stdin
	command.Stdout = input.Stdout
	command.Stderr = input.Stderr
	result, err := client.runner.Run(ctx, command)
	return result, client.commandError(ctx, op, err)
}
