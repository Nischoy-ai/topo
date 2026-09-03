package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"github.com/Nischoy-ai/topo/internal/servicenowpackage"
)

func main() {
	input := flag.String("in", "", "ServiceNow SDK pack ZIP")
	output := flag.String("out", "", "new normalized app ZIP")
	flag.Parse()
	if *input == "" || *output == "" || flag.NArg() != 0 {
		log.Fatal("usage: servicenowpackagetool -in <sdk-zip> -out <new-zip>")
	}
	metadata, err := servicenowpackage.Normalize(*input, *output)
	if err != nil {
		log.Fatalf("ServiceNow package normalization failed: %v", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(metadata); err != nil {
		log.Fatalf("encode ServiceNow package metadata: %v", err)
	}
}
