package main

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func shouldPing(lastSlot, key string) bool {
	return lastSlot != key
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	stateFile := getenv("STATE_FILE", "/data/last_slot")
	logPath := getenv("LOG_FILE", "/data/reset.log")

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("cannot open log file", "path", logPath, "error", err.Error())
		os.Exit(1)
	}
	defer logFile.Close()

	logger := slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stdout, logFile), nil))

	key := time.Now().Format("2006-01-02_15")

	last, _ := os.ReadFile(stateFile)
	if !shouldPing(string(last), key) {
		logger.Info("skip", "slot", key, "reason", "already pinged")
		return
	}

	start := time.Now()
	cmd := exec.Command("claude", "-p", "hi")
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	if err != nil {
		logger.Error("ping failed", "slot", key, "duration_ms", duration.Milliseconds(), "error", err.Error(), "output", string(output))
		os.Exit(1)
	}

	logger.Info("ping ok", "slot", key, "duration_ms", duration.Milliseconds())

	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		logger.Error("failed to create state dir", "error", err.Error())
		os.Exit(1)
	}
	if err := os.WriteFile(stateFile, []byte(key), 0644); err != nil {
		logger.Error("failed to write state file", "error", err.Error())
		os.Exit(1)
	}
}
