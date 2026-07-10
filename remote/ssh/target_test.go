package ssh_test

import (
	"errors"
	"testing"

	lpkgo "github.com/lib-x/lpk-go"
	sshremote "github.com/lib-x/lpk-go/remote/ssh"
)

func TestParseTargetMatchesLZCCLIAddressRules(t *testing.T) {
	tests := []struct {
		address string
		host    string
		port    int
		boxName string
	}{
		{address: "box.example", host: "box.example", port: 22, boxName: "box.example"},
		{address: "box.example:2222", host: "box.example", port: 2222, boxName: "box.example:2222"},
		{address: "[2001:db8::1]:2222", host: "2001:db8::1", port: 2222, boxName: "2001:db8::1:2222"},
		{address: "2001:db8::1", host: "2001:db8::1", port: 22, boxName: "2001:db8::1"},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			target, err := sshremote.ParseTarget("developer", test.address)
			if err != nil {
				t.Fatal(err)
			}
			if target.User != "developer" || target.Host != test.host || target.Port != test.port || target.BoxName != test.boxName {
				t.Fatalf("target = %#v", target)
			}
			if target.SSHAddress() != "developer@"+test.host {
				t.Fatalf("SSHAddress() = %q", target.SSHAddress())
			}
		})
	}
}

func TestParseTargetRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct{ user, address string }{
		{user: "", address: "box"},
		{user: "developer", address: ""},
		{user: "developer", address: ":22"},
		{user: "developer", address: "box:0"},
		{user: "developer", address: "box:65536"},
		{user: "developer", address: "[2001:db8::1"},
	} {
		if _, err := sshremote.ParseTarget(test.user, test.address); !errors.Is(err, lpkgo.ErrInvalidArgument) {
			t.Fatalf("ParseTarget(%q, %q) error = %#v", test.user, test.address, err)
		}
	}
}
