package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/Nischoy-ai/topo/internal/release"
)

func main() {
	mode := flag.String("mode", "build", "build, refresh-metadata, profile, or policy")
	root := flag.String("root", ".", "repository source root")
	output := flag.String("out", "", "new output directory")
	version := flag.String("version", "", "semantic release version, including leading v")
	commit := flag.String("commit", "", "source commit digest, or dev")
	profile := flag.String("profile", "", "release profile: all, linux-macos-beta, or linux-homebrew-beta")
	flag.Parse()
	if *mode == "profile" || *mode == "policy" {
		var policy release.Policy
		var err error
		if *output != "" {
			policy, err = release.ReadPolicy(*output, *version)
		} else {
			policy, err = release.PolicyForProfile(*profile, *version)
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("include_windows=%t\n", policy.IncludeWindows)
		if *mode == "policy" {
			fmt.Printf("require_apple_signing=%t\n", policy.RequireAppleSigning)
		}
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
