// Command minecraft-instance-manager-gui is the graphical launcher.
//
// It is a separate binary from the CLI because Gio needs cgo on Linux and
// macOS; keeping it apart is what lets the CLI go on cross-compiling for every
// target from one host with CGO_ENABLED=0.
package main

import (
	"flag"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/gui"
)

// Version is stamped at build time with
// -ldflags "-X main.Version=v2.0.0".
var Version = "v2.0.0-dev"

// defaultMSAClientID is the Azure application id used for Microsoft sign-in,
// injected at build time. Without one the launcher offers local accounts only.
var defaultMSAClientID = ""

func main() {
	flag.Parse()

	gui.RunMain(gui.Options{
		Version:    Version,
		LauncherID: defaultMSAClientID,
	})
}
