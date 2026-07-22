package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Tr0sT/multica-declarative/internal/backend"
	"github.com/Tr0sT/multica-declarative/internal/config"
	"github.com/Tr0sT/multica-declarative/internal/exporter"
	"github.com/Tr0sT/multica-declarative/internal/model"
	"github.com/Tr0sT/multica-declarative/internal/reconcile"
)

var Version = "0.3.0-dev"

func Run(args []string, stdout, stderr io.Writer) int {
	command, flagArgs, err := splitCommand(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	flags := flag.NewFlagSet("multica-declarative", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "multica.yaml", "path to the workspace manifest")
	multicaBinary := flags.String("multica-bin", "multica", "path or name of the Multica CLI binary")
	outputDir := flags.String("output-dir", "multica-export", "directory written by export")
	force := flags.Bool("force", false, "replace generated export paths in a non-empty output directory")
	showVersion := flags.Bool("version", false, "print the version")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: multica-declarative [flags] <export|validate|plan|apply>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "error: unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, Version)
		return 0
	}
	if command == "" {
		flags.Usage()
		return 2
	}

	cliBackend := backend.NewCLI(*multicaBinary)
	if command == "export" {
		result, err := (exporter.Exporter{Backend: cliBackend}).Export(exporter.Options{
			OutputDir: *outputDir,
			Force:     *force,
		})
		if err != nil {
			fmt.Fprintf(stderr, "export failed: %v\n", err)
			return 1
		}
		for _, warning := range result.Warnings {
			fmt.Fprintf(stderr, "warning: %s\n", warning)
		}
		fmt.Fprintf(
			stdout,
			"Exported %d skill(s), %d agent(s), and %d runtime selector(s) to %s.\n",
			result.Skills,
			result.Agents,
			result.Runtimes,
			result.OutputDir,
		)
		return 0
	}

	project, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "%s failed: %v\n", command, err)
		return 1
	}
	if command == "validate" {
		fmt.Fprintf(
			stdout,
			"Configuration is valid: %d skill(s), %d agent(s), %d runtime selector(s).\n",
			len(project.Skills),
			len(project.Agents),
			len(project.RuntimeSelectors),
		)
		return 0
	}

	controller := reconcile.Reconciler{Backend: cliBackend}
	switch command {
	case "plan":
		changes, err := controller.Plan(project)
		if err != nil {
			fmt.Fprintf(stderr, "plan failed: %v\n", err)
			return 1
		}
		printPlan(stdout, changes)
		return 0
	case "apply":
		err := controller.Apply(project, func(change model.Change) {
			fmt.Fprintln(stdout, reconcile.FormatChange(change))
		})
		if err != nil {
			fmt.Fprintf(stderr, "apply failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Apply complete.")
		return 0
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n", command)
		return 2
	}
}

func printPlan(writer io.Writer, changes []model.Change) {
	counts := map[string]int{reconcile.Create: 0, reconcile.Update: 0, reconcile.Noop: 0}
	for _, change := range changes {
		fmt.Fprintln(writer, reconcile.FormatChange(change))
		counts[change.Action]++
	}
	fmt.Fprintf(
		writer,
		"\nPlan: %d to create, %d to update, %d unchanged.\n",
		counts[reconcile.Create],
		counts[reconcile.Update],
		counts[reconcile.Noop],
	)
}

func splitCommand(args []string) (string, []string, error) {
	var command string
	remaining := make([]string, 0, len(args))
	expectsValue := false
	for _, argument := range args {
		if expectsValue {
			remaining = append(remaining, argument)
			expectsValue = false
			continue
		}
		if argument == "--config" || argument == "--multica-bin" || argument == "--output-dir" {
			remaining = append(remaining, argument)
			expectsValue = true
			continue
		}
		if argument == "export" || argument == "validate" || argument == "plan" || argument == "apply" {
			if command != "" {
				return "", nil, fmt.Errorf("multiple commands: %q and %q", command, argument)
			}
			command = argument
			continue
		}
		remaining = append(remaining, argument)
	}
	if expectsValue {
		return "", nil, fmt.Errorf("flag requires a value")
	}
	return command, remaining, nil
}
