package main

import (
	"flag"
	"log"

	"github.com/mohxmmad/ihdl/internal"
)

func main() {
	inputFile := flag.String("iinp", "", "input values file")
	outputFile := flag.String("iout", "", "output values file")
	displayFile := flag.String("ippf", "", "export rendered display frame(s) to .ippf")
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatal("usage: ihdl <file.ihdl>")
	}
	input := flag.Arg(0)
	if (*inputFile == "") != (*outputFile == "") {
		log.Fatal("both -iinp and -iout must be provided together")
	}

	project, err := internal.ParseProject(input)
	if err != nil {
		log.Fatal(err)
	}

	if *inputFile != "" {
		err = internal.SimulateFromFilesWithOptions(project, *inputFile, *outputFile, internal.SimulationOptions{DisplayExportPath: *displayFile})
	} else {
		err = internal.SimulateWithOptions(project, internal.SimulationOptions{DisplayExportPath: *displayFile})
	}
	if err != nil {
		log.Fatal(err)
	}
}
