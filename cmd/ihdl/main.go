package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	input := flag.String("i", "", "Input iHDL file")
	flag.Parse()

	if *input == "" {
		fmt.Println("Usage: ihdl -i <file.ihdl>")
		os.Exit(1)
	}

	fmt.Println("Loading:", *input)

	data, err := os.ReadFile(*input)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println("\n========== SOURCE ==========")
	fmt.Println(string(data))
	fmt.Println("============================")
}
