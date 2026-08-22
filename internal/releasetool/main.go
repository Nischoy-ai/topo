package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/Nischoy-ai/topo/internal/release"
)

func main() {
	root := flag.String("root", ".", "repository source root")
	output := flag.String("out", "", "new output directory")
	version := flag.String("version", "", "semantic release version, including leading v")
	commit := flag.String("commit", "", "source commit digest, or dev")
	flag.Parse()
	if err := release.Build(context.Background(), release.Options{
		Root:      *root,
		OutputDir: *output,
		Version:   *version,
		Commit:    *commit,
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("built reproducible %s release artifacts in %s\n", release.RuntimeTarget(), *output)
}
