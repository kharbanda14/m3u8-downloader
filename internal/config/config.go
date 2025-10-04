package config

import "time"

// Config holds the downloader configuration
type Config struct {
	URL           string
	OutputDir     string
	Output        string
	MaxRetry      int
	Threads       int
	Timeout       time.Duration
	ValidateFiles bool // Option to validate integrity of downloaded segments
}

// New creates a new Config instance with the provided parameters
func New(url, outputDir, output string, maxRetry, threads int, timeout time.Duration, validateFiles bool) *Config {
	return &Config{
		URL:           url,
		OutputDir:     outputDir,
		Output:        output,
		MaxRetry:      maxRetry,
		Threads:       threads,
		Timeout:       timeout,
		ValidateFiles: validateFiles,
	}
}
