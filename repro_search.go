//go:build tools
// +build tools

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/yourorg/infractl/internal/web"
)

func main() {
	fmt.Println("Testing Web Search...")
	results, err := web.Search(context.Background(), "test", 5)
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}

	fmt.Printf("Found %d results\n", len(results))
	for i, r := range results {
		fmt.Printf("%d. %s (Len: %d, URL: %s)\n", i+1, r.Title, len(r.Title), r.URL)
	}
}

