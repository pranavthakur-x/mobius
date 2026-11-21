// Command guides_preview loads workspace guide files and prints the rendered
// system prompt to stdout.
package main

import (
	"fmt"
	"os"

	"mobius/internal/guides"
)

func main() {
	gs, err := guides.LoadFromWorkSpace("config/guides.toml", ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "guides_preview: load workspace guides: %v\n", err)
		os.Exit(1)
	}

	// Print the rendered markdown prompt generated from the workspace.
	fmt.Println(gs.RenderSystemPrompt())
}
