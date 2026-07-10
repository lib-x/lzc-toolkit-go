package ssh

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const defaultPort = 22

type Target struct {
	BoxName string
	User    string
	Host    string
	Port    int
}

func ParseTarget(loginUser, address string) (Target, error) {
	user := strings.TrimSpace(loginUser)
	if user == "" {
		return Target{}, sshError(lpkgo.CodeInvalidArgument, "remote.ssh.parse_target", errors.New("login user is required"))
	}
	host, port, err := parseAddress(address)
	if err != nil {
		return Target{}, err
	}
	boxName := host
	if port != defaultPort {
		boxName = fmt.Sprintf("%s:%d", host, port)
	}
	return Target{BoxName: boxName, User: user, Host: host, Port: port}, nil
}

func (target Target) SSHAddress() string {
	if target.User == "" || target.Host == "" {
		return ""
	}
	return target.User + "@" + target.Host
}

func parseAddress(address string) (string, int, error) {
	value := strings.TrimSpace(address)
	if value == "" {
		return "", 0, sshError(lpkgo.CodeInvalidArgument, "remote.ssh.parse_target", errors.New("address is required"))
	}
	if strings.HasPrefix(value, "[") {
		end := strings.IndexByte(value, ']')
		if end <= 1 {
			return "", 0, invalidAddress()
		}
		host := strings.TrimSpace(value[1:end])
		suffix := strings.TrimSpace(value[end+1:])
		if host == "" {
			return "", 0, invalidAddress()
		}
		if suffix == "" {
			return host, defaultPort, nil
		}
		if !strings.HasPrefix(suffix, ":") {
			return "", 0, invalidAddress()
		}
		port, err := parsePort(suffix[1:])
		return host, port, err
	}

	switch strings.Count(value, ":") {
	case 0:
		return value, defaultPort, nil
	case 1:
		index := strings.LastIndexByte(value, ':')
		host := strings.TrimSpace(value[:index])
		if host == "" {
			return "", 0, invalidAddress()
		}
		port, err := parsePort(strings.TrimSpace(value[index+1:]))
		return host, port, err
	default:
		return value, defaultPort, nil
	}
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, invalidAddress()
	}
	return port, nil
}

func invalidAddress() error {
	return sshError(lpkgo.CodeInvalidArgument, "remote.ssh.parse_target", errors.New("invalid SSH address"))
}

func sshError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
