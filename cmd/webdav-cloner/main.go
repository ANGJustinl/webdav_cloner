package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"webdav_cloner/internal/cloner"
	"webdav_cloner/internal/config"
)

func main() {
	var (
		configPath  string
		dryRun      bool
		concurrency = runtime.NumCPU()
		noProgress  bool
	)

	flag.StringVar(&configPath, "config", "", "Path to a YAML configuration file")
	flag.BoolVar(&dryRun, "dry-run", false, "Print actions without copying data")
	flag.IntVar(&concurrency, "concurrency", concurrency, "Concurrent file copy workers per job")
	flag.BoolVar(&noProgress, "no-progress", false, "Disable progress bar output")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s --config <path> [options]\n\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if configPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	options := cloner.Options{
		DryRun:       dryRun,
		Concurrency:  concurrency,
		Logger:       logger,
		ShowProgress: !noProgress,
	}

	if err := cloner.Run(ctx, cfg, options); err != nil {
		logger.Printf("clone failed: %v", err)
		os.Exit(1)
	}
}
