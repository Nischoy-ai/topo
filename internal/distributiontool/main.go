package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Nischoy-ai/topo/internal/distribution"
)

func main() {
	artifacts := flag.String("artifacts", "", "verified release artifact directory")
	out := flag.String("out", "", "new distribution output directory")
	version := flag.String("version", "", "semantic release tag")
	channel := flag.String("channel", "", "stable or beta")
	releaseURL := flag.String("release-base-url", "https://github.com/Nischoy-ai/topo", "release repository URL")
	repositoryURL := flag.String("repository-base-url", "https://nischoy-ai.github.io/topo-packages", "native repository URL")
	publishedAt := flag.String("published-at", "", "whole-second RFC3339 promotion timestamp")
	flag.Parse()

	timestamp, err := time.Parse(time.RFC3339, *publishedAt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid -published-at: %v\n", err)
		os.Exit(2)
	}
	if err := distribution.Build(distribution.Options{
		ArtifactDir:       *artifacts,
		OutputDir:         *out,
		Version:           *version,
		Channel:           *channel,
		ReleaseBaseURL:    *releaseURL,
		RepositoryBaseURL: *repositoryURL,
		PublishedAt:       timestamp,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
