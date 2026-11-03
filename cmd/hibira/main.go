// Command hibira sends and watches realtime events on Hibira.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/hibira-io/cli/internal/cli"
)

// Set by the release build with -ldflags "-X main.version=... -X main.commit=...".
var (
	version = ""
	commit  = ""
)

func main() {
	b := cli.Build{Version: version, Commit: commit}
	if b.Version == "" {
		b.Version = "dev"
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			b.Version = info.Main.Version
		}
	}
	if b.Commit == "" {
		b.Commit = "unknown"
	}
	if err := cli.New(b).ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
