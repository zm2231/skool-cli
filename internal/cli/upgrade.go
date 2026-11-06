// Copyright 2026 Zain Haseeb and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"
)

const releaseRepo = "zm2231/skool-cli"

func newUpgradeCmd(flags *rootFlags) *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade skool-pp-cli (and skool-pp-mcp beside it) to the latest release",
		Long: `Download the latest release from github.com/` + releaseRepo + `, verify it against the
release's checksums.txt, and replace this binary in place. If skool-pp-mcp sits in the
same folder, it is upgraded too.`,
		Example: "  skool-pp-cli upgrade --check\n  skool-pp-cli upgrade",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*flags.timeout)
			defer cancel()

			updater, err := selfupdate.NewUpdater(selfupdate.Config{
				Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
			})
			if err != nil {
				return err
			}
			latest, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(releaseRepo))
			if err != nil {
				return fmt.Errorf("checking for a new release: %w", err)
			}
			if !found {
				return fmt.Errorf("no release of %s for %s/%s", releaseRepo, runtime.GOOS, runtime.GOARCH)
			}

			current := version
			upToDate := false
			if _, perr := semver.NewVersion(current); perr == nil {
				upToDate = latest.LessOrEqual(current)
			}
			out := cmd.OutOrStdout()
			if flags.asJSON {
				if checkOnly || upToDate {
					return printJSONFiltered(out, map[string]any{
						"current":          current,
						"latest":           latest.Version(),
						"update_available": !upToDate,
						"release_url":      latest.URL,
					}, flags)
				}
			}
			if upToDate {
				fmt.Fprintf(out, "skool-pp-cli %s is the latest release.\n", current)
				return nil
			}
			if checkOnly {
				fmt.Fprintf(out, "skool-pp-cli %s is available (you have %s). Run: skool-pp-cli upgrade\n", latest.Version(), current)
				return nil
			}
			exe, err := selfupdate.ExecutablePath()
			if err != nil {
				return fmt.Errorf("locating this binary: %w", err)
			}
			mcpName := "skool-pp-mcp"
			if runtime.GOOS == "windows" {
				mcpName += ".exe"
			}
			targets := []string{}
			if mcp := filepath.Join(filepath.Dir(exe), mcpName); fileExists(mcp) {
				targets = append(targets, mcp)
			}
			// The CLI goes last: its version is what marks the upgrade done, so a
			// failure on any earlier binary leaves the CLI old and a rerun retries.
			targets = append(targets, exe)

			if flags.dryRun {
				if flags.asJSON {
					return printJSONFiltered(out, map[string]any{
						"current":     current,
						"latest":      latest.Version(),
						"asset_url":   latest.AssetURL,
						"would_write": targets,
					}, flags)
				}
				fmt.Fprintf(out, "would upgrade %v from %s to %s (%s)\n", targets, current, latest.Version(), latest.AssetURL)
				return nil
			}

			upgraded := []string{}
			for _, target := range targets {
				if err := updater.UpdateTo(ctx, latest, target); err != nil {
					if errors.Is(err, os.ErrPermission) {
						return fmt.Errorf("cannot replace %s (permission denied); rerun with permission to write that folder: %w", target, err)
					}
					return fmt.Errorf("upgrading %s to %s: %w", target, latest.Version(), err)
				}
				upgraded = append(upgraded, target)
			}

			if flags.asJSON {
				return printJSONFiltered(out, map[string]any{
					"previous": current,
					"current":  latest.Version(),
					"upgraded": upgraded,
				}, flags)
			}
			for _, p := range upgraded {
				fmt.Fprintf(out, "upgraded %s to %s\n", p, latest.Version())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only report whether a newer release exists")
	return cmd
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
