package main

import (
	"log"

	"github.com/ca-srg/kiberag/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
