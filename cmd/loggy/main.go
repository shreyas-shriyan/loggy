// Command loggy is an interactive log viewer for EKS and ECS services on AWS.
package main

import (
	"fmt"
	"os"

	"github.com/shreyas-shriyan/loggy/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
