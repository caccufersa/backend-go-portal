package main

import (
	"cacc/pkg/database/migrations"
	"fmt"
	"io/fs"
)

func main() {
	files, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Found %d files in '.'\n", len(files))
	for _, f := range files {
		fmt.Printf("- %s (dir: %v)\n", f.Name(), f.IsDir())
	}
}
