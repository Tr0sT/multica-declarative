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
	"github.com/Tr0sT/multica-declarative/internal/workspace"
)

var Version = "0.5.0-dev"

func Run(args []string, stdout, stderr io.Writer) int {
	command, flagArgs, err := splitCommand(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	flags := flag.NewFlagSet("multica-declarative", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "multica.yaml", "path to workspace or workspace-set manifest")
	binary := flags.String("multica-bin", "multica", "Multica CLI binary")
	outputDir := flags.String("output-dir", "multica-export", "directory written by export")
	force := flags.Bool("force", false, "replace generated export paths")
	withoutSecrets := flags.Bool("without-secrets", false, "omit agent env, MCP, runtime config and custom args during export; leave them unmanaged during validate/plan/apply")
	allWorkspaces := flags.Bool("all-workspaces", false, "export all accessible workspaces into <slug>/ directly under the output directory")
	profile := flags.String("profile", "", "Multica CLI profile for this invocation")
	workspaceID := flags.String("workspace-id", "", "explicit target for a single-workspace declaration")
	version := flags.Bool("version", false, "print version")
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
	if *version {
		fmt.Fprintln(stdout, Version)
		return 0
	}
	if command == "" {
		flags.Usage()
		return 2
	}
	invalidScope := false
	flags.Visit(func(f *flag.Flag) {
		if (f.Name == "profile" || f.Name == "workspace-id") && strings.TrimSpace(f.Value.String()) == "" {
			fmt.Fprintf(stderr, "error: --%s must not be empty\n", f.Name)
			invalidScope = true
		}
	})
	if invalidScope {
		return 2
	}
	if *allWorkspaces && (command != "export" || *workspaceID != "") {
		fmt.Fprintln(stderr, "error: --all-workspaces is only valid with export and cannot be combined with --workspace-id")
		return 2
	}
	rawCLI := backend.NewCLI(*binary)
	cli := rawCLI.WithScope(*profile, *workspaceID)
	if command == "export" {
		options := exporter.Options{OutputDir: *outputDir, Force: *force, WithoutSecrets: *withoutSecrets}
		if *allWorkspaces {
			ex := exporter.WorkspaceExporter{
				Catalog:    rawCLI.WithScope(*profile, ""),
				BackendFor: func(id string) backend.Backend { return rawCLI.WithScope(*profile, id) },
			}
			result, err := ex.Export(options)
			if err != nil {
				fmt.Fprintf(stderr, "export failed: %v\n", err)
				return 1
			}
			printExport(stdout, stderr, result.Result)
			fmt.Fprintf(stdout, "Exported %d workspace(s) with explicit workspace bindings.\n", result.Workspaces)
			return 0
		}
		id, omit, err := flatExportTarget(*outputDir, *workspaceID)
		if err != nil {
			fmt.Fprintf(stderr, "export failed: %v\n", err)
			return 1
		}
		options.WithoutSecrets = options.WithoutSecrets || omit
		cli = rawCLI.WithScope(*profile, id)
		result, err := (exporter.Exporter{Backend: cli}).Export(options)
		if err != nil {
			fmt.Fprintf(stderr, "export failed: %v\n", err)
			return 1
		}
		printExport(stdout, stderr, result)
		return 0
	}
	set, err := workspace.Load(*configPath, config.LoadOptions{WithoutSecrets: *withoutSecrets})
	if err != nil {
		fmt.Fprintf(stderr, "%s failed: %v\n", command, err)
		return 1
	}
	if set != nil {
		if *workspaceID != "" && (len(set.Entries) != 1 || set.Entries[0].ID != *workspaceID) {
			fmt.Fprintln(stderr, "error: --workspace-id cannot override workspace-set bindings; select a child with --config instead")
			return 2
		}
		catalog := rawCLI.WithScope(*profile, "")
		factory := func(id string) backend.Backend { return rawCLI.WithScope(*profile, id) }
		if err := runWorkspaceSet(command, set, catalog, factory, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "%s failed: %v\n", command, err)
			return 1
		}
		return 0
	}
	project, err := config.LoadWithOptions(*configPath, config.LoadOptions{WithoutSecrets: *withoutSecrets})
	if err != nil {
		fmt.Fprintf(stderr, "%s failed: %v\n", command, err)
		return 1
	}
	if project.WithoutSecrets {
		fmt.Fprintln(stderr, "notice: agent custom environment, MCP configuration, runtime config and custom arguments are unmanaged; existing values will not be changed")
	}
	if command == "validate" {
		printValidation(stdout, project)
		return 0
	}
	controller := reconcile.Reconciler{Backend: cli}
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
		if err := controller.Apply(project, func(c model.Change) { fmt.Fprintln(stdout, reconcile.FormatChange(c)) }); err != nil {
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

func printValidation(w io.Writer, project model.Project) {
	fmt.Fprintf(w, "Configuration is valid: %d skill(s), %d agent(s), %d squad(s), %d project(s), %d autopilot(s), %d runtime selector(s).\n", len(project.Skills), len(project.Agents), len(project.Squads), len(project.Projects), len(project.Autopilots), len(project.RuntimeSelectors))
}

func printExport(stdout, stderr io.Writer, result exporter.Result) {
	for _, warning := range result.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", warning)
	}
	fmt.Fprintf(stdout, "Exported %d skill(s), %d agent(s), %d squad(s), %d project(s), %d autopilot(s), and %d runtime selector(s) to %s.\n", result.Skills, result.Agents, result.Squads, result.Projects, result.Autopilots, result.Runtimes, result.OutputDir)
}

func printPlan(w io.Writer, changes []model.Change) {
	counts := map[string]int{reconcile.Create: 0, reconcile.Update: 0, reconcile.Noop: 0}
	for _, c := range changes {
		fmt.Fprintln(w, reconcile.FormatChange(c))
		counts[c.Action]++
	}
	fmt.Fprintf(w, "\nPlan: %d to create, %d to update, %d unchanged.\n", counts[reconcile.Create], counts[reconcile.Update], counts[reconcile.Noop])
}
func splitCommand(args []string) (string, []string, error) {
	var command string
	remaining := []string{}
	expects := false
	for _, a := range args {
		if expects {
			remaining = append(remaining, a)
			expects = false
			continue
		}
		if a == "--config" || a == "--multica-bin" || a == "--output-dir" || a == "--profile" || a == "--workspace-id" {
			remaining = append(remaining, a)
			expects = true
			continue
		}
		if a == "export" || a == "validate" || a == "plan" || a == "apply" {
			if command != "" {
				return "", nil, fmt.Errorf("multiple commands")
			}
			command = a
			continue
		}
		remaining = append(remaining, a)
	}
	if expects {
		return "", nil, fmt.Errorf("flag requires a value")
	}
	return command, remaining, nil
}
