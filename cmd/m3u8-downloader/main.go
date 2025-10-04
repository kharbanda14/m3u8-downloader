package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"m3u8-downloader/internal/config"
	"m3u8-downloader/internal/downloader"
)

func main() {
	// Parse command line arguments
	m3u8URL := flag.String("url", "", "M3U8 playlist URL (required)")
	outputDir := flag.String("dir", "downloads", "Directory for temporary files")
	output := flag.String("output", "output.ts", "Output file name")
	maxRetry := flag.Int("retry", 5, "Max retry times when download fails")
	threads := flag.Int("threads", 10, "Number of concurrent downloads")
	timeout := flag.Int("timeout", 30, "Timeout in seconds for HTTP requests")
	validate := flag.Bool("validate", true, "Validate integrity of downloaded segments")
	flag.Parse()

	if *m3u8URL == "" {
		fmt.Println("Error: M3U8 URL is required")
		flag.Usage()
		os.Exit(1)
	}

	// Create configuration
	cfg := config.New(
		*m3u8URL,
		*outputDir,
		*output,
		*maxRetry,
		*threads,
		time.Duration(*timeout)*time.Second,
		*validate,
	)

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		fmt.Printf("Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Create downloader and start downloading
	dl := downloader.New(cfg)
	if err := dl.Download(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Download completed successfully!")
}
