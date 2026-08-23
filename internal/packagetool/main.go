package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/Nischoy-ai/topo/internal/packagebuild"
)

func main() {
	mode := flag.String("mode", "build", "operation: build, bundle, checksums, or finalize")
	root := flag.String("root", ".", "repository root")
	raw := flag.String("raw", "", "verified raw release artifact directory")
	out := flag.String("out", "", "new output directory or bundle path")
	version := flag.String("version", "", "semantic release tag")
	nfpm := flag.String("nfpm", "", "path to the pinned nFPM binary")
	flag.Parse()

	var err error
	switch *mode {
	case "build":
		err = packagebuild.Build(context.Background(), packagebuild.Options{
			Root:       *root,
			RawDir:     *raw,
			OutputDir:  *out,
			Version:    *version,
			NFPMBinary: *nfpm,
		})
	case "bundle":
		err = packagebuild.WriteOfflineBundle(*root, *raw, *out, *version)
	case "checksums":
		dir := *out
		if dir == "" {
			dir = *raw
		}
		if dir == "" {
			err = fmt.Errorf("artifact directory is required")
		} else {
			err = packagebuild.FinalizeChecksums(filepath.Clean(dir))
		}
	case "finalize":
		dir := *out
		if dir == "" {
			dir = *raw
		}
		if dir == "" {
			err = fmt.Errorf("artifact directory is required")
		} else if err = packagebuild.RefreshPackageMetadata(filepath.Clean(dir)); err == nil {
			err = packagebuild.FinalizeChecksums(filepath.Clean(dir))
		}
	default:
		err = fmt.Errorf("unsupported mode %q", *mode)
	}
	if err != nil {
		log.Printf("package assembly failed: %v", err)
		os.Exit(1)
	}
}
