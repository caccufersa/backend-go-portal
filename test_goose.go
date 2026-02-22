package main

import (
	"fmt"
	"log"

	"cacc/pkg/database/migrations"

	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

func main() {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("[DB] goose dialect err: %v", err)
	}

	files, err := goose.CollectMigrations(".", 0, (1<<63)-1)
	if err != nil {
		log.Fatalf("Collect error: %v", err)
	}
	fmt.Printf("Collected %d migrations\n", len(files))
	for _, f := range files {
		fmt.Printf(" - %v\n", f.Source)
	}
}
