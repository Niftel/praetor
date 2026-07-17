package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/praetordev/praetor/internal/readiness"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("readiness-report", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "path to the validation evidence manifest")
	output := flags.String("output", "", "path for the sanitized readiness report (stdout when empty)")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if *input == "" {
		return fail(stderr, "-input is required")
	}

	f, err := os.Open(*input)
	if err != nil {
		return fail(stderr, err.Error())
	}
	manifest, err := readiness.Decode(f)
	_ = f.Close()
	if err != nil {
		return fail(stderr, err.Error())
	}
	report, err := readiness.Generate(manifest)
	if err != nil {
		return fail(stderr, err.Error())
	}

	w := stdout
	var outputFile *os.File
	if *output != "" {
		outputFile, err = os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return fail(stderr, err.Error())
		}
		w = outputFile
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		if outputFile != nil {
			_ = outputFile.Close()
		}
		return fail(stderr, err.Error())
	}
	if outputFile != nil {
		if err := outputFile.Close(); err != nil {
			return fail(stderr, err.Error())
		}
	}
	if report.Decision.Status != "go" {
		return 2
	}
	return 0
}

func fail(stderr io.Writer, message string) int {
	fmt.Fprintln(stderr, "error:", message)
	return 1
}
