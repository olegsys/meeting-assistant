package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/olegsys/meeting-assistant/internal/cli"
)

func main() {
	fs := flag.NewFlagSet("cli", flag.ContinueOnError)

	serviceAddress := fs.String("service-address", envOrDefault("SERVICE_ADDRESS", "http://localhost:8080"), "service address")
	defaultUser := fs.String("user", envOrDefault("DEFAULT_USER", ""), "default user id")

	_ = fs.Parse(os.Args[1:])

	args := fs.Args()

	if len(args) == 0 {
		printUsage()
		os.Exit(2)
	}

	runner := cli.NewRunner(*serviceAddress, *defaultUser)

	if err := runner.Run(context.Background(), args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	return fallback
}

func printUsage() {
	fmt.Println(`Usage:
  cli [global flags] <command> [command flags]

Global flags:
  -service-address string
        service address (default "http://localhost:8080")
  -user string
        default user id

Commands:
  start
  load -path <path>
  list
  status -id <id>
  get -id <id>
  find -keyword <keyword>
  chat -id <id> -text <text>
  retry -id <id>`)
}
