// Command validate-project is an internal/manual compatibility harness. It is
// not a public SDK CLI and is not exercised by ordinary go test ./....
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/image/dockerlocal"
	"github.com/lib-x/lzc-toolkit-go/project"
)

func main() {
	root := flag.String("root", ".", "project root")
	config := flag.String("config", build.DefaultConfigFile, "build configuration")
	localImages := flag.Bool("local-images", false, "enable the local Docker image adapter")
	flag.Parse()
	inspection, err := project.Inspect(context.Background(), project.InspectRequest{
		Root:       *root,
		ConfigFile: *config,
	})
	if err != nil {
		fatal(err)
	}
	fmt.Printf("INSPECT %s: kind=%s layout=%s package=%s version=%s services=%d images=%d templated=%t diagnostics=%d\n",
		filepath.Clean(*root), inspection.Kind, inspection.Layout, inspection.Package.Package,
		inspection.Package.Version, len(inspection.Services), len(inspection.Images),
		inspection.Template.Present, len(inspection.Diagnostics))

	loaded, err := build.LoadConfig(context.Background(), *root, *config, nil)
	if err != nil {
		fatal(err)
	}
	if loaded.Config.Images != nil && !*localImages {
		fmt.Printf("SKIP %s: image stage required\n", filepath.Clean(*root))
		return
	}
	request := build.Request{
		Root:       *root,
		ConfigFile: *config,
	}
	if *localImages {
		request.ImageBuilder = dockerlocal.New(nil)
	}
	result, err := build.Build(context.Background(), io.Discard, request)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("OK %s: layout=%s package=%s version=%s warnings=%d\n",
		filepath.Clean(*root), result.Layout, result.Package, result.Version, len(result.Warnings))
}

func fatal(err error) {
	var sdkError *lpkgo.Error
	if errors.As(err, &sdkError) {
		fmt.Fprintf(os.Stderr, "ERROR code=%s op=%s path=%s: %v\n", sdkError.Code, sdkError.Op, sdkError.Path, sdkError.Cause)
	} else {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	}
	os.Exit(1)
}
