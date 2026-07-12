package main

import (
	"flag"
	"log"

	"github.com/mohxmmad/ihdl/internal"
)

func main() {
	input := flag.String("i", "", "entry .ihdl file")
	inputFile := flag.String("iinp", "", "input values file")
	outputFile := flag.String("iout", "", "output values file")
	flag.Parse()

	if *input == "" {
		log.Fatal("usage: ihdl -i file.ihdl")
	}
	if (*inputFile == "") != (*outputFile == "") {
		log.Fatal("both -iinp and -iout must be provided together")
	}

	project, err := internal.ParseProject(*input)
	if err != nil {
		log.Fatal(err)
	}

	if *inputFile != "" {
		err = internal.SimulateFromFiles(project, *inputFile, *outputFile)
	} else {
		err = internal.Simulate(project)
	}
	if err != nil {
		log.Fatal(err)
	}
}
