// Command grip is the deterministic control plane that keeps a human the
// architect while AI agents implement. See the plan/ folder and reqs/ for the
// full intent. This file is only the process entrypoint; all behavior lives in
// internal/cli and the engine packages.
package main

import (
	"os"

	"github.com/artembatutin/grip/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
