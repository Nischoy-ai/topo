package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Nischoy-ai/topo/internal/productioncheck"
)

func main() {
	owner := flag.String("owner", "Nischoy-ai", "GitHub organization")
	repository := flag.String("repository", "topo", "source repository")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "production preflight does not accept positional arguments")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	report, err := productioncheck.Run(ctx, productioncheck.GHAPI{}, productioncheck.Options{
		Owner:      *owner,
		Repository: *repository,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "production preflight failed:", err)
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, "production preflight failed to encode its report")
		os.Exit(2)
	}
	if !report.Ready {
		os.Exit(1)
	}
}
