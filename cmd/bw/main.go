package main

import (
	"os"

	"github.com/typicalfo/bullwhip/internal/bw"
)

func main() { os.Exit(bw.Run(os.Args[1:], os.Stdout, os.Stderr)) }
