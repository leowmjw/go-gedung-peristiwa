package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/leow/go-gedung-peristiwa/internal/web/sitessmoke"
)

func main() {
	out := flag.String("out", "tmp/sites-smoke/public", "directory for generated static site files")
	flag.Parse()

	if err := sitessmoke.Generate(*out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("generated Datastar sanity site in %s\n", *out)
}
