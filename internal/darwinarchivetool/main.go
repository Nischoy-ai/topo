package main

import (
	"flag"
	"log"

	"github.com/Nischoy-ai/topo/internal/release"
)

func main() {
	input := flag.String("input", "", "directory containing signed topo, LICENSE, and README.md")
	output := flag.String("out", "", "new deterministic tar.gz path")
	version := flag.String("version", "", "semantic release tag")
	arch := flag.String("arch", "", "amd64 or arm64")
	flag.Parse()
	if err := release.RearchiveDarwin(*input, *output, *version, *arch); err != nil {
		log.Fatal(err)
	}
}
