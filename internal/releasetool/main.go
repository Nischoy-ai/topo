package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/Nischoy-ai/topo/internal/release"
)

func main() {
	mode := flag.String("mode", "build", "build, refresh-metadata, or profile")
	root := flag.String("root", ".", "repository source root")
	output := flag.String("out", "", "new output directory")
	version := flag.String("version", "", "semantic release version, including leading v")
	commit := flag.String("commit", "", "source commit digest, or dev")
	profile := flag.String("profile", "", "release profile: all or linux-macos-beta")
	flag.Parse()
	if *mode == "profile" {
		includeWindows := *profile != release.LinuxMacOSBeta
		var err error
		if *output != "" {
			includeWindows, err = release.ReadProfile(*output, *version)
		} else {
			_, err = release.TargetsForProfile(*profile, *version)
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("include_windows=%t\n", includeWindows)
		return
	}
	if *mode == "refresh-metadata" {
		if err := release.RefreshMetadata(*output); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("refreshed release metadata in %s\n", *output)
		return
	}
	if *mode != "build" {
		log.Fatalf("unsupported mode %q", *mode)
	}
	if err := release.Build(context.Background(), release.Options{
		Profile:   *profile,
		Root:      *root,
		OutputDir: *output,
		Version:   *version,
		Commit:    *commit,
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("built reproducible %s release artifacts in %s\n", release.RuntimeTarget(), *output)
}
