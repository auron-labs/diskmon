package main

import (
	"fmt"
	"os"

	"diskmon/internal/cli"
	"diskmon/internal/config"
	"diskmon/internal/util"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger, levelVar, err := util.NewLogger(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger error: %v\n", err)
		os.Exit(1)
	}

	if err := cli.NewRootCmd(cfg, logger, levelVar).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
