package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	stateFile      = "/data/last_slot"
	logPath        = "/data/reset.log"
	binPath        = "/app/reset"
	crontabPath    = "/app/crontab"
	supercronicBin = "/usr/local/bin/supercronic"
	defaultHours   = "6,11,16,21"
)

func shouldPing(lastSlot, key string) bool {
	return lastSlot != key
}

// buildCrontab is what -init writes to crontabPath. The SHELL= line points
// supercronic at this binary instead of /bin/sh — see runShellCommand.
func buildCrontab(hours string) string {
	return fmt.Sprintf("SHELL=%s\n0 %s * * * %s\n", binPath, hours, binPath)
}

func main() {
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("cannot open log file", "path", logPath, "error", err.Error())
		os.Exit(1)
	}
	defer logFile.Close()

	logger := slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stdout, logFile), nil))

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-init":
			runInit(logger, os.Args[2:])
			return
		case "-c":
			runShellCommand(logger, os.Args[2])
			return
		}
	}

	runPing(logger)
}

// runInit is the container ENTRYPOINT. It regenerates the crontab from
// RESET_HOURS on every start (so `docker compose restart` applies a changed
// schedule without a rebuild) and execs supercronic. Extra args (from
// `docker compose run claude-reset <cmd>`) are exec'd directly instead, for
// ad-hoc debugging without spinning up the real cron loop.
func runInit(logger *slog.Logger, debugArgs []string) {
	if len(debugArgs) > 0 {
		execOrExit(logger, debugArgs[0], debugArgs)
		return
	}

	hours := os.Getenv("RESET_HOURS")
	if hours == "" {
		hours = defaultHours
	}
	if err := os.WriteFile(crontabPath, []byte(buildCrontab(hours)), 0644); err != nil {
		logger.Error("cannot write crontab", "path", crontabPath, "error", err.Error())
		os.Exit(1)
	}

	execOrExit(logger, supercronicBin, []string{supercronicBin, crontabPath})
}

// runShellCommand stands in for "sh -c <command>" — supercronic runs every
// scheduled job through the interpreter named by the crontab's SHELL= line
// (see aptible/supercronic crontab/crontab.go), which runInit points at
// this binary so the image never needs a real shell.
func runShellCommand(logger *slog.Logger, command string) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		logger.Error("empty scheduled command")
		os.Exit(1)
	}
	execOrExit(logger, fields[0], fields)
}

// syscall.Exec doesn't search PATH, so bare names like `claude` (from
// `docker compose run claude-reset claude --version`) are resolved first.
func execOrExit(logger *slog.Logger, path string, argv []string) {
	resolved, err := exec.LookPath(path)
	if err == nil {
		err = syscall.Exec(resolved, argv, os.Environ())
	}
	if err != nil {
		logger.Error("exec failed", "path", path, "error", err.Error())
		os.Exit(1)
	}
}

func runPing(logger *slog.Logger) {
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

	if err := os.WriteFile(stateFile, []byte(key), 0644); err != nil {
		logger.Error("failed to write state file", "error", err.Error())
		os.Exit(1)
	}
}
