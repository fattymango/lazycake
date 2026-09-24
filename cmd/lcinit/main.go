// Command lcinit is a static binary bind-mounted into task containers as
// PID 1. It execs the customer's real entrypoint as a child, enforces
// max_duration with a hard exit regardless of the agent or host, and
// forwards signals to the child. See PLAN.md "Killing orphans".
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("lcinit dev")
		return
	}

	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "lcinit:", err)
	}
	os.Exit(code)
}
