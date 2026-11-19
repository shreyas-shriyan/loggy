// Package cli defines loggy's commands.
package cli

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/shreyas-shriyan/loggy/internal/config"
	"github.com/shreyas-shriyan/loggy/internal/stream"
	"github.com/shreyas-shriyan/loggy/internal/tui"
	"github.com/spf13/cobra"
)

// Version is set at build time with -ldflags.
var Version = "dev"

// Execute runs the root command.
func Execute() error {
	return quietBack(newRoot().Execute())
}

func newRoot() *cobra.Command {
	var configPath string
	app := &App{out: os.Stdout}

	root := &cobra.Command{
		Use:   "loggy [dev|prod]",
		Short: "Interactive log viewer for EKS and ECS services on AWS",
		Long: `loggy streams and saves logs for saved AWS targets.

Run "loggy" to pick an environment, or "loggy dev" / "loggy prod" to open
the dashboard for it directly. In the dashboard press enter to stream the
selected server and q to quit; the bottom bar lists all keys.`,
		Args:          cobra.MaximumNArgs(1),
		ValidArgs:     config.Environments,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			path := configPath
			if path == "" {
				p, err := config.DefaultPath()
				if err != nil {
					return err
				}
				path = p
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			if err := stream.SetHealthPatterns(cfg.HealthPatterns); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			app.init(cfg)
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			env, err := envArg(args)
			if err != nil {
				return err
			}
			return quietBack(app.dashboard(env, nil))
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file (default $LOGGY_CONFIG or ~/.config/loggy/config.yaml)")

	root.AddCommand(
		&cobra.Command{
			Use:       "setup [dev|prod]",
			Short:     "Add saved EKS or ECS targets",
			Args:      cobra.MaximumNArgs(1),
			ValidArgs: config.Environments,
			RunE: func(_ *cobra.Command, args []string) error {
				env, err := envArg(args)
				if err != nil {
					return err
				}
				return quietBack(app.setup(env))
			},
		},
		&cobra.Command{
			Use:       "menu [dev|prod]",
			Short:     "Browse clusters and services without saving a target",
			Args:      cobra.MaximumNArgs(1),
			ValidArgs: config.Environments,
			RunE: func(_ *cobra.Command, args []string) error {
				env, err := envArg(args)
				if err != nil {
					return err
				}
				return quietBack(app.adhocMenu(env))
			},
		},
		newTargetsCmd(app),
		&cobra.Command{
			Use:   "version",
			Short: "Print the loggy version",
			Args:  cobra.NoArgs,
			Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), "loggy", Version) },
		},
	)
	return root
}

func newTargetsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "targets",
		Short: "List or remove saved targets",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:       "list [dev|prod]",
			Short:     "List saved targets",
			Args:      cobra.MaximumNArgs(1),
			ValidArgs: config.Environments,
			RunE: func(cmd *cobra.Command, args []string) error {
				envs := config.Environments
				if len(args) == 1 {
					if !config.ValidEnv(args[0]) {
						return fmt.Errorf("unknown environment %q", args[0])
					}
					envs = args
				}
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "ENV\tNAME\tDETAILS")
				n := 0
				for _, env := range envs {
					for _, t := range app.cfg.TargetsFor(env) {
						fmt.Fprintf(w, "%s\t%s\t%s\n", env, t.Primary(), t.Secondary(app.cfg.RegionFor(t)))
						n++
					}
				}
				if err := w.Flush(); err != nil {
					return err
				}
				if n == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No saved targets. Run `loggy setup` to add one.")
				}
				return nil
			},
		},
		&cobra.Command{
			Use:       "rm [dev|prod]",
			Short:     "Remove saved targets interactively",
			Args:      cobra.MaximumNArgs(1),
			ValidArgs: config.Environments,
			RunE: func(_ *cobra.Command, args []string) error {
				env, err := envArg(args)
				if err != nil {
					return err
				}
				return quietBack(app.removeTargets(env))
			},
		},
	)
	return cmd
}

// envArg returns the environment from args, prompting when absent.
func envArg(args []string) (string, error) {
	if len(args) == 1 {
		if !config.ValidEnv(args[0]) {
			return "", fmt.Errorf("unknown environment %q (use dev or prod)", args[0])
		}
		return args[0], nil
	}
	return tui.SelectStrings("Environment", config.Environments, nil)
}

func quietBack(err error) error {
	if errors.Is(err, tui.ErrBack) {
		return nil
	}
	return err
}
