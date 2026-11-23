package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/jinzhu/configor"
	"github.com/spf13/cobra"

	"github.com/clusterpedia-io/clusterpedia/pkg/storage/internalstorage"
)

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if errors.Is(err, internalstorage.ErrSchemaOutOfDate) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "internalstorage-schema",
		Short:         "Manage the database schema of the internal storage",
		SilenceUsage:  true,
		SilenceErrors: true,

		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}

	var config string
	addConfigFlag := func(c *cobra.Command) {
		c.Flags().StringVar(&config, "config", "", "Path to the config file of the internal storage.")

		// configor only prints a message to stdout when the file doesn't exist.
		run := c.RunE
		c.RunE = func(cmd *cobra.Command, args []string) error {
			if config != "" {
				if _, err := os.Stat(config); err != nil {
					return err
				}
			}
			return run(cmd, args)
		}
	}

	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Check whether the schema is up to date without modifying the database, exit with 2 if it is out of date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := internalstorage.CheckSchema(config); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "the schema is up to date")
			return nil
		},
	}
	addConfigFlag(checkCmd)
	_ = checkCmd.MarkFlagRequired("config")

	var dryRun bool
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Bring the schema of the database up to date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dryRun {
				statements, err := internalstorage.PendingSchemaSQL(config)
				if err != nil {
					return err
				}
				printStatements(cmd.OutOrStdout(), statements)
				return nil
			}

			if err := internalstorage.MigrateSchema(config); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "the schema is up to date")
			return nil
		},
	}
	addConfigFlag(migrateCmd)
	_ = migrateCmd.MarkFlagRequired("config")
	migrateCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Only print the statements that would be executed.")

	var dbType string
	printCmd := &cobra.Command{
		Use:   "print",
		Short: "Print the statements that create the schema from scratch, without connecting to the database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if config != "" {
				cfg := &internalstorage.Config{}
				if err := configor.Load(cfg, config); err != nil {
					return err
				}
				dbType = cfg.Type
			}

			statements, err := internalstorage.SchemaSQL(dbType)
			if err != nil {
				return err
			}
			printStatements(cmd.OutOrStdout(), statements)
			return nil
		},
	}
	addConfigFlag(printCmd)
	printCmd.Flags().StringVar(&dbType, "type", "", "Type of the database: postgres, mysql or sqlite, read from the config file if --config is set.")
	printCmd.MarkFlagsOneRequired("config", "type")
	printCmd.MarkFlagsMutuallyExclusive("config", "type")

	cmd.AddCommand(checkCmd, migrateCmd, printCmd)
	return cmd
}

func printStatements(out io.Writer, statements []string) {
	for _, statement := range statements {
		fmt.Fprintf(out, "%s;\n", statement)
	}
}
