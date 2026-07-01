package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// modulePath is what `bmo upgrade` reinstalls. It matches go.mod.
const modulePath = "github.com/justin06lee/bmo"

func newUpgradeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade bmo to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			goBin, err := exec.LookPath("go")
			if err != nil {
				return errors.New("upgrading needs the Go toolchain (bmo is installed with `go install`); get it from https://go.dev/dl")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Current version: %s\n", buildVersion())
			install := exec.Command(goBin, "install", modulePath+"@latest")
			install.Stdout = cmd.OutOrStdout()
			install.Stderr = cmd.ErrOrStderr()
			if err := install.Run(); err != nil {
				return fmt.Errorf("go install %s@latest failed: %w", modulePath, err)
			}
			installDir := goInstallDir(goBin)
			if installDir != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Installed the latest bmo to %s\n", filepath.Join(installDir, "bmo"))
				if exe, err := os.Executable(); err == nil {
					if resolved, err := filepath.EvalSymlinks(exe); err == nil {
						exe = resolved
					}
					if filepath.Dir(exe) != installDir {
						fmt.Fprintf(cmd.OutOrStdout(), "Note: this run used %s, which was not replaced. Make sure %s comes first in your PATH.\n", exe, installDir)
					}
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Run `bmo init` to refresh the bundled bmo skill.")
			return nil
		},
	}
}

// buildVersion reports the module version stamped into the binary by
// `go install` ("(devel)" for source builds).
func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "unknown"
}

// goInstallDir returns the directory `go install` writes binaries to: GOBIN if
// set, otherwise GOPATH/bin. Empty string if it can't be determined.
func goInstallDir(goBin string) string {
	out, err := exec.Command(goBin, "env", "GOBIN").Output()
	if err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			return dir
		}
	}
	out, err = exec.Command(goBin, "env", "GOPATH").Output()
	if err != nil {
		return ""
	}
	gopath := strings.TrimSpace(string(out))
	if gopath == "" {
		return ""
	}
	return filepath.Join(gopath, "bin")
}
