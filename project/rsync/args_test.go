package rsync_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/project/rsync"
)

func TestBuildArgsMatchesLZCCLIForUserAppAndSubdirectory(t *testing.T) {
	root := t.TempDir()
	ignorePath := filepath.Join(root, ".lzcdevignore")
	if err := os.WriteFile(ignorePath, []byte("node_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, err := rsync.BuildArgs(rsync.Options{
		RootDir: root, SourceDir: "web/src", Delete: true, DryRun: true, Debug: true,
		Target: rsync.Target{
			UID: "user-1", Host: "2001:db8::1", PackageID: "cloud.example.app", UserApp: true,
			Directory: "/lzcapp/cache/project-mirror/assets",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--recursive", "--links", "--times", "--perms", "--omit-dir-times", "--human-readable", "--itemize-changes", "--compress",
		"-P", "--delete", "--dry-run", "--exclude-from=" + ignorePath, "--relative",
		"./web/src/", "rsync://user-1@[2001:db8::1]:874/lzcapp_cache/cloud.example.app/user-1/project-mirror/assets/",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args=%#v\nwant=%#v", args, want)
	}
}

func TestBuildArgsUsesDefaultTargetAndIPv4(t *testing.T) {
	args, err := rsync.BuildArgs(rsync.Options{
		RootDir: t.TempDir(),
		Target:  rsync.Target{UID: "user-1", Host: "127.0.0.1", PackageID: "cloud.example.app"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTail := []string{"./", "rsync://user-1@127.0.0.1:874/lzcapp_cache/cloud.example.app/project-mirror/"}
	if !reflect.DeepEqual(args[len(args)-2:], wantTail) {
		t.Fatalf("tail=%#v", args[len(args)-2:])
	}
}

func TestBuildTunnelArgsPreservesSSHOptions(t *testing.T) {
	args, err := rsync.BuildTunnelArgs(rsync.TunnelOptions{
		SSHArgs:   []string{"-o", "StrictHostKeyChecking=no", "-p", "2222", "developer@box.example"},
		LocalPort: 19000, TargetHost: "fd00::8",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-o", "StrictHostKeyChecking=no", "-p", "2222",
		"-o", "ExitOnForwardFailure=yes", "-L", "127.0.0.1:19000:[fd00::8]:874",
		"developer@box.example", "-N",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args=%#v", args)
	}
}

func TestBuildArgsRejectsTargetOutsideCacheAndAbsoluteSource(t *testing.T) {
	for _, options := range []rsync.Options{
		{
			RootDir: t.TempDir(), SourceDir: "/etc",
			Target: rsync.Target{UID: "user-1", Host: "box.example", PackageID: "cloud.example.app"},
		},
		{
			RootDir: t.TempDir(),
			Target:  rsync.Target{UID: "user-1", Host: "box.example", PackageID: "cloud.example.app", Directory: "/data"},
		},
	} {
		if _, err := rsync.BuildArgs(options); !errors.Is(err, lpkgo.ErrInvalidArgument) {
			t.Fatalf("error=%#v", err)
		}
	}
}
