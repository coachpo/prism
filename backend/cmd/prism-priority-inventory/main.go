package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/coachpo/prism/backend/internal/platform/priority"
)

type commandOptions struct {
	format             string
	failOnUnclassified bool
	patterns           []string
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		slog.Error("priority inventory failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	return runWithInventory(args, stdout, priority.DefaultInventory())
}

func runWithInventory(args []string, stdout io.Writer, inventory priority.Inventory) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	if len(options.patterns) == 0 {
		return errors.New("at least one package pattern is required")
	}
	if options.format != "markdown" {
		return fmt.Errorf("unsupported format %q: only markdown is supported", options.format)
	}

	if options.failOnUnclassified {
		if err := priority.ValidateInventory(inventory); err != nil {
			return err
		}
	}
	return priority.WriteMarkdownInventory(stdout, inventory)
}

func parseOptions(args []string) (commandOptions, error) {
	flags := flag.NewFlagSet("prism-priority-inventory", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := commandOptions{}
	flags.StringVar(&options.format, "format", "markdown", "output format")
	flags.BoolVar(&options.failOnUnclassified, "fail-on-unclassified", false, "exit nonzero when inventory contains unclassified entries")
	if err := flags.Parse(args); err != nil {
		return commandOptions{}, err
	}
	options.patterns = flags.Args()
	return options, nil
}
