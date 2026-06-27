package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/justin06lee/bmo/internal/bmo"
	"github.com/justin06lee/bmo/internal/cli"
)

// embeddedSkills bundles the bmo skill into the binary so `bmo add bmo`,
// `bmo init`, and first-run auto-install work offline.
//
//go:embed all:skills/bmo
var embeddedSkills embed.FS

func main() {
	if sub, err := fs.Sub(embeddedSkills, "skills/bmo"); err == nil {
		bmo.SetEmbeddedFS(sub)
	}
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
