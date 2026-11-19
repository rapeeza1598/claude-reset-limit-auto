package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strconv"
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
	defaultModel   = "haiku"
	// supercronic never overlaps runs of a job, so a hung ping would block
	// every later slot until the container restarts.
	pingTimeout = 5 * time.Minute
	// a scheduled fire later than this past the hour is skipped, see onTime
	lateGrace = 10 * time.Minute
	// a fire this close before an hour belongs to that hour, see slotStart
	earlyGrace = time.Minute
)

func shouldPing(lastSlot, key string) bool {
	return lastSlot != key
}

// slotStart returns the hour a run belongs to. On the Windows host (Docker
// Desktop, WSL2) supercronic routinely fires 1-3s before the hour, e.g.
// 10:59:57 for the 11:00 job, so a run within earlyGrace of the next hour
// counts toward that hour.
func slotStart(now time.Time) time.Time {
	t := now.Add(earlyGrace)
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

// onTime reports whether a cron fire at now is close enough to a scheduled
// hour to ping. When the host sleeps (Docker's VM on Mac or Windows gets
// paused), supercronic runs the overdue job on wake, hours late, and a late
// ping would start the 5-hour window at the wrong time.
func onTime(now time.Time, hours []int) bool {
	slot := slotStart(now)
	return slices.Contains(hours, slot.Hour()) && now.Sub(slot) < lateGrace
}

// parseHours validates RESET_HOURS ("6,11,16,21"). Before this, a bad value
// silently produced a broken crontab.
func parseHours(s string) ([]int, error) {
	var hours []int
	for _, f := range strings.Split(s, ",") {
		h, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || h < 0 || h > 23 {
			return nil, fmt.Errorf("RESET_HOURS: %q is not an hour 0-23", f)
		}
		hours = append(hours, h)
	}
	return hours, nil
}

func resetHours() string {
	if h := os.Getenv("RESET_HOURS"); h != "" {
		return h
	}
	return defaultHours
}

// buildCrontab is what -init writes to crontabPath. The SHELL= line points
// supercronic at this binary instead of /bin/sh — see runShellCommand.
// -scheduled turns on the onTime check, so manual runs still ping at any hour.
func buildCrontab(hours []int) string {
	fields := make([]string, len(hours))
	for i, h := range hours {
		fields[i] = strconv.Itoa(h)
	}
	return fmt.Sprintf("SHELL=%s\n0 %s * * * %s -scheduled\n", binPath, strings.Join(fields, ","), binPath)
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
		case "-scheduled":
			runPing(logger, true)
			return
		}
	}

	runPing(logger, false)
}

// runInit is the container ENTRYPOINT. It regenerates the crontab from
// RESET_HOURS on every start (so `docker compose up -d` applies a changed
// schedule without a rebuild) and execs supercronic. Extra args (from
// `docker compose run claude-reset <cmd>`) are exec'd directly instead, for
// ad-hoc debugging without spinning up the real cron loop.
func runInit(logger *slog.Logger, debugArgs []string) {
	if len(debugArgs) > 0 {
		execOrExit(logger, debugArgs[0], debugArgs)
		return
	}

	hours, err := parseHours(resetHours())
	if err != nil {
		logger.Error("invalid RESET_HOURS", "error", err.Error())
		os.Exit(1)
	}
	if err := os.WriteFile(crontabPath, []byte(buildCrontab(hours)), 0644); err != nil {
		logger.Error("cannot write crontab", "path", crontabPath, "error", err.Error())
		os.Exit(1)
	}
	logger.Info("crontab written", "hours", hours)

	// -passthrough-logs keeps the job's JSON lines intact (otherwise they're
	// escaped inside supercronic's own msg field); -json makes its own lines
	// JSON too, so `docker compose logs` is one format.
	execOrExit(logger, supercronicBin, []string{supercronicBin, "-json", "-passthrough-logs", crontabPath})
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

func runPing(logger *slog.Logger, scheduled bool) {
	now := time.Now()
	key := slotStart(now).Format("2006-01-02_15")

	if scheduled {
		// -init already rejected a bad RESET_HOURS; if it somehow slips
		// through, hours is nil and the fire is skipped, never sent late.
		hours, _ := parseHours(resetHours())
		if !onTime(now, hours) {
			logger.Info("skip", "slot", key, "reason", "late fire, host was probably asleep")
			return
		}
	}

	last, _ := os.ReadFile(stateFile)
	if !shouldPing(string(last), key) {
		logger.Info("skip", "slot", key, "reason", "already pinged")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	// The cheapest model is enough: any message starts the 5-hour window.
	// PING_MODEL in .env switches it back (e.g. sonnet) without a rebuild,
	// in case haiku isn't available on the account.
	model := os.Getenv("PING_MODEL")
	if model == "" {
		model = defaultModel
	}

	start := time.Now()
	// --tools "" and --no-session-persistence keep the ping small: no tool
	// definitions in the prompt, no session file written. Never add --bare:
	// it ignores CLAUDE_CODE_OAUTH_TOKEN entirely.
	cmd := exec.CommandContext(ctx, "claude", "-p", "hi", "--model", model, "--tools", "", "--no-session-persistence")
	// claude may leave children holding the output pipe after a kill;
	// don't let that stall Wait past the timeout.
	cmd.WaitDelay = 10 * time.Second
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)
	if err != nil && ctx.Err() != nil {
		err = ctx.Err() // "context deadline exceeded" beats "signal: killed"
	}

	if err != nil {
		logger.Error("ping failed", "slot", key, "model", model, "duration_ms", duration.Milliseconds(), "error", err.Error(), "output", string(output))
		os.Exit(1)
	}

	logger.Info("ping ok", "slot", key, "model", model, "duration_ms", duration.Milliseconds())

	if err := os.WriteFile(stateFile, []byte(key), 0644); err != nil {
		logger.Error("failed to write state file", "error", err.Error())
		os.Exit(1)
	}
}
