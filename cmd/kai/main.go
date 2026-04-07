package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/c4xx/kai/internal/config"
	"github.com/c4xx/kai/internal/core"
	"github.com/c4xx/kai/internal/memory"
	"github.com/c4xx/kai/internal/safety"
	"github.com/c4xx/kai/internal/scheduler"
	"github.com/robfig/cron/v3"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "init":
		runInit()
	case "install":
		runInstall()
	case "run":
		runJob(args)
	case "daemon":
		runDaemon()
	case "status":
		runStatus()
	case "briefing":
		runBriefing()
	case "log":
		runLog(args)
	case "confirm":
		runConfirm(args)
	case "reject":
		runReject(args)
	case "pending":
		runPending()
	case "why":
		runWhy(args)
	case "replay":
		runReplay(args)
	case "version":
		fmt.Println("kai v0.2.0")
	default:
		fmt.Fprintf(os.Stderr, "kai: unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "kai — your always-on developer companion")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage: kai <command> [args]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  init        Set up kai (API keys, repos, schedule)")
	fmt.Fprintln(os.Stderr, "  install     Install launchd/systemd service")
	fmt.Fprintln(os.Stderr, "  daemon      Run the daemon (foreground)")
	fmt.Fprintln(os.Stderr, "  run [job]   Run a job immediately (default: github_summary)")
	fmt.Fprintln(os.Stderr, "  status      Show daemon and budget status")
	fmt.Fprintln(os.Stderr, "  briefing    Print the latest briefing")
	fmt.Fprintln(os.Stderr, "  log         Show recent action audit log")
	fmt.Fprintln(os.Stderr, "  pending     List pending confirmations")
	fmt.Fprintln(os.Stderr, "  confirm <id> [--force]  Confirm a pending action")
	fmt.Fprintln(os.Stderr, "  reject <id>  Reject a pending action")
	fmt.Fprintln(os.Stderr, "  why <run-id> Show all actions for a run")
	fmt.Fprintln(os.Stderr, "  replay <run-id>  Read-only simulation of a prior run")
}

// mustLoadConfig loads config and exits on error.
func mustLoadConfig() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kai: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

// mustOpenDB opens the database and exits on error.
func mustOpenDB(cfg *config.Config) *memory.DB {
	if err := config.EnsureDataDirs(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "kai: creating data dirs: %v\n", err)
		os.Exit(1)
	}
	db, err := memory.Open(context.Background(), cfg.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kai: opening database: %v\n", err)
		os.Exit(1)
	}
	return db
}

// --- Commands ---

func runInit() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("kai init — setting up your developer companion")
	fmt.Println()

	// 1. Anthropic API key
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Print("Enter your Anthropic API key (sk-ant-...): ")
		apiKey = readLine(reader)
	} else {
		fmt.Printf("Using ANTHROPIC_API_KEY from environment.\n")
	}
	if !strings.HasPrefix(apiKey, "sk-ant-") {
		fmt.Fprintln(os.Stderr, "Warning: API key doesn't look like an Anthropic key (expected sk-ant-...)")
	}

	// 2. GitHub token
	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" {
		fmt.Print("Enter your GitHub personal access token (needs: repo, read:user): ")
		ghToken = readLine(reader)
	} else {
		fmt.Printf("Using GITHUB_TOKEN from environment.\n")
	}

	// 3. Watch repos
	fmt.Print("Watch which repos? (owner/repo, comma-separated, or Enter to skip): ")
	repoLine := readLine(reader)
	var watchRepos []string
	for _, r := range strings.Split(repoLine, ",") {
		r = strings.TrimSpace(r)
		if r != "" {
			watchRepos = append(watchRepos, r)
		}
	}

	// 4. Schedule
	fmt.Print("Daily briefing time? [9:00 AM]: ")
	timeLine := strings.TrimSpace(readLine(reader))
	schedule := "0 9 * * *"
	if timeLine != "" {
		// Basic parsing: "HH:MM AM/PM" → cron
		schedule = parseCronFromTime(timeLine)
	}

	// 5. Write config
	configDir, err := config.DefaultConfigDir()
	if err != nil {
		die("getting config dir: %v", err)
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		die("creating config dir: %v", err)
	}

	repoList := ""
	if len(watchRepos) > 0 {
		quoted := make([]string, len(watchRepos))
		for i, r := range watchRepos {
			quoted[i] = fmt.Sprintf("%q", r)
		}
		repoList = strings.Join(quoted, ", ")
	}

	configContent := fmt.Sprintf(`github_token = %q
anthropic_api_key = %q
schedule = %q
watch_repos = [%s]
github_poll_interval = "60s"
briefing_feedback = false

[trust]
state_change = "confirm"

[limits]
max_tokens_context = 8000
daily_token_budget = 100000
github_requests_per_hour = 60

[paths]
data_dir = ""
`, ghToken, apiKey, schedule, repoList)

	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		die("writing config: %v", err)
	}
	fmt.Printf("Config written to %s\n", configPath)

	// 6. Test run
	fmt.Print("\nSend a test briefing now? (press Ctrl-C to skip) [Y/n]: ")
	yn := strings.TrimSpace(readLine(reader))
	if yn == "" || strings.ToLower(yn) == "y" {
		cfg, err := config.Load()
		if err != nil {
			die("loading config: %v", err)
		}
		db := mustOpenDB(cfg)
		defer db.Close()

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		fmt.Println("Running github_summary...")
		if err := core.Run(ctx, cfg, db, "github_summary"); err != nil {
			fmt.Fprintf(os.Stderr, "Test run failed: %v\n", err)
		} else {
			fmt.Println("Briefing delivered. Run `kai briefing` to read it.")
		}
	}

	// 7. Install prompt
	fmt.Print("\nRun `kai install` to start the daemon on login? [Y/n]: ")
	yn = strings.TrimSpace(readLine(reader))
	if yn == "" || strings.ToLower(yn) == "y" {
		runInstall()
	}
}

func runInstall() {
	cfg := mustLoadConfig()
	exe, err := os.Executable()
	if err != nil {
		die("resolving executable path: %v", err)
	}

	switch runtime.GOOS {
	case "darwin":
		installLaunchd(cfg, exe)
	case "linux":
		installSystemd(cfg, exe)
	default:
		fmt.Fprintln(os.Stderr, "kai install: unsupported OS (macOS and Linux only)")
		os.Exit(1)
	}
}

func installLaunchd(cfg *config.Config, exe string) {
	label := "com.kai.daemon"
	plistPath := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", label+".plist")

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s/kai.log</string>
	<key>StandardErrorPath</key>
	<string>%s/kai.log</string>
</dict>
</plist>
`, label, exe, cfg.DataDir, cfg.DataDir)

	launchAgentsDir := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		die("creating LaunchAgents dir: %v", err)
	}
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		die("writing plist: %v", err)
	}
	fmt.Printf("Wrote %s\n", plistPath)

	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: launchctl load failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Run manually: launchctl load %s\n", plistPath)
		return
	}
	fmt.Println("kai daemon installed and started. It will run on login.")
}

func installSystemd(cfg *config.Config, exe string) {
	home := os.Getenv("HOME")
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	unitPath := filepath.Join(unitDir, "kai.service")

	unit := fmt.Sprintf(`[Unit]
Description=kai developer companion daemon
After=network.target

[Service]
ExecStart=%s daemon
Restart=on-failure
StandardOutput=append:%s/kai.log
StandardError=append:%s/kai.log

[Install]
WantedBy=default.target
`, exe, cfg.DataDir, cfg.DataDir)

	if err := os.MkdirAll(unitDir, 0755); err != nil {
		die("creating systemd user dir: %v", err)
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		die("writing unit file: %v", err)
	}
	fmt.Printf("Wrote %s\n", unitPath)

	if err := exec.Command("systemctl", "--user", "enable", "--now", "kai").Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: systemctl enable failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Run manually: systemctl --user enable --now kai\n")
		return
	}
	fmt.Println("kai daemon enabled and started. It will run on login.")
}

func runJob(args []string) {
	job := "github_summary"
	if len(args) > 0 {
		job = args[0]
	}

	cfg := mustLoadConfig()
	db := mustOpenDB(cfg)
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Printf("kai: running %s...\n", job)
	if err := core.Run(ctx, cfg, db, job); err != nil {
		fmt.Fprintf(os.Stderr, "kai: run failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("kai: run completed.")
}

func runDaemon() {
	cfg := mustLoadConfig()
	db := mustOpenDB(cfg)
	defer db.Close()

	// Write PID file.
	pidPath := filepath.Join(cfg.DataDir, "daemon.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "kai: writing PID: %v\n", err)
	}
	defer os.Remove(pidPath)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Reconcile pending actions from previous run.
	reconcilePending(cfg, db)

	d := scheduler.New(cfg, db)
	if err := d.Start(ctx); err != nil {
		die("starting scheduler: %v", err)
	}
	defer d.Stop()

	fmt.Printf("kai daemon running (pid %d), schedule: %s\n", os.Getpid(), cfg.Schedule)
	<-ctx.Done()
	fmt.Println("kai daemon shutting down.")
}

func reconcilePending(cfg *config.Config, db *memory.DB) {
	pendingDir := filepath.Join(cfg.DataDir, "pending")
	entries, err := os.ReadDir(pendingDir)
	if err != nil {
		return
	}
	aborted := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		run, err := db.GetRunForAction(id)
		if err != nil || run == nil || run.Status != "in_progress" {
			db.LogActionAbort(id, "daemon-restarted")
			os.Remove(filepath.Join(pendingDir, e.Name()))
			aborted++
		}
	}
	if aborted > 0 {
		fmt.Printf("kai: %d stale pending action(s) aborted on startup\n", aborted)
	}
}

func runStatus() {
	cfg := mustLoadConfig()
	db := mustOpenDB(cfg)
	defer db.Close()

	// Daemon liveness
	pidPath := filepath.Join(cfg.DataDir, "daemon.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		fmt.Println("daemon: not running")
	} else {
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if err := syscall.Kill(pid, 0); err != nil {
			fmt.Println("daemon: stale PID file (not running)")
		} else {
			fmt.Printf("daemon: running (pid %d)\n", pid)
		}
	}

	// Next scheduled run
	sched, err := cron.ParseStandard(cfg.Schedule)
	if err == nil {
		next := sched.Next(time.Now())
		fmt.Printf("next run: %s\n", next.Format("2006-01-02 15:04:05"))
	}

	// Token budget
	used, _ := db.TokensUsedToday()
	remaining := int64(cfg.Limits.DailyTokenBudget) - used
	fmt.Printf("token budget: %d used / %d remaining today\n", used, remaining)

	// Pending confirmations
	gate := safety.NewGate(cfg.DataDir, cfg.Trust.StateChange)
	ids, _ := gate.ListPending()
	fmt.Printf("pending confirmations: %d\n", len(ids))
	if len(ids) > 0 {
		fmt.Println("  Run `kai pending` to review.")
	}

	// Last run
	runs, _ := db.LatestRuns(1)
	if len(runs) > 0 {
		r := runs[0]
		ts := time.Unix(r.TS, 0).Format("2006-01-02 15:04:05")
		tokens := int64(0)
		if r.TokensUsed != nil {
			tokens = *r.TokensUsed
		}
		fmt.Printf("last run: %s at %s (%d tokens)\n", r.Job, ts, tokens)
	}
}

func runBriefing() {
	cfg := mustLoadConfig()
	db := mustOpenDB(cfg)
	defer db.Close()

	briefingDir := filepath.Join(cfg.DataDir, "briefings")
	entries, err := os.ReadDir(briefingDir)
	if err != nil || len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "No briefings yet. Run `kai run github_summary`.")
		os.Exit(1)
	}

	// Find latest .md file.
	var mdFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			mdFiles = append(mdFiles, e.Name())
		}
	}
	if len(mdFiles) == 0 {
		fmt.Fprintln(os.Stderr, "No briefings yet. Run `kai run github_summary`.")
		os.Exit(1)
	}
	sort.Strings(mdFiles)
	latest := mdFiles[len(mdFiles)-1]

	data, err := os.ReadFile(filepath.Join(briefingDir, latest))
	if err != nil {
		die("reading briefing: %v", err)
	}
	fmt.Println(string(data))

	// Mark briefing_opened for the corresponding run.
	// Extract run ID from filename: YYYY-MM-DD-<runid8>.md
	parts := strings.Split(strings.TrimSuffix(latest, ".md"), "-")
	if len(parts) >= 4 {
		runIDPrefix := parts[3]
		// Find the run by prefix match (first 8 chars of UUID).
		runs, _ := db.LatestRuns(10)
		for _, r := range runs {
			if strings.HasPrefix(r.ID, runIDPrefix) {
				db.MarkBriefingOpened(r.ID)
				break
			}
		}
	}
}

func runLog(args []string) {
	cfg := mustLoadConfig()
	db := mustOpenDB(cfg)
	defer db.Close()

	limit := 20
	runFilter := ""
	for i, arg := range args {
		if arg == "--limit" && i+1 < len(args) {
			if n, err := strconv.Atoi(args[i+1]); err == nil {
				limit = n
			}
		}
		if arg == "--run" && i+1 < len(args) {
			runFilter = args[i+1]
		}
	}

	var actions []*memory.Action
	var err error
	if runFilter != "" {
		actions, err = db.ActionsForRun(runFilter)
	} else {
		actions, err = db.RecentActions(limit)
	}
	if err != nil {
		die("querying actions: %v", err)
	}
	if len(actions) == 0 {
		fmt.Println("No actions logged yet.")
		return
	}

	for _, a := range actions {
		ts := time.Unix(a.TS, 0).Format("2006-01-02 15:04:05")
		confirmed := ""
		if a.Confirmed == 1 {
			confirmed = " [confirmed]"
		}
		blastColor := blastRadiusColor(a.BlastRadius)
		fmt.Printf("%s  %s%-16s%s  %s%s\n",
			ts, blastColor, a.BlastRadius, resetColor, a.Tool, confirmed)
	}
}

func runConfirm(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: kai confirm <id> [--force]")
		os.Exit(1)
	}
	id := args[0]
	force := false
	for _, a := range args[1:] {
		if a == "--force" {
			force = true
		}
	}

	cfg := mustLoadConfig()
	gate := safety.NewGate(cfg.DataDir, cfg.Trust.StateChange)
	if err := gate.Confirm(id, force); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Println("OK, action confirmed.")
}

func runReject(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: kai reject <id>")
		os.Exit(1)
	}
	id := args[0]

	cfg := mustLoadConfig()
	gate := safety.NewGate(cfg.DataDir, cfg.Trust.StateChange)
	if err := gate.Reject(id); err != nil {
		fmt.Fprintf(os.Stderr, "kai: reject failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Action rejected.")
}

func runPending() {
	cfg := mustLoadConfig()
	gate := safety.NewGate(cfg.DataDir, cfg.Trust.StateChange)

	ids, err := gate.ListPending()
	if err != nil {
		die("listing pending: %v", err)
	}
	if len(ids) == 0 {
		fmt.Println("No pending confirmations.")
		return
	}

	for _, id := range ids {
		a, err := gate.ReadPending(id)
		if err != nil {
			fmt.Printf("  %s: (error reading: %v)\n", id, err)
			continue
		}
		age := time.Since(a.CreatedAt)
		stale := ""
		if age > 24*time.Hour {
			stale = " [STALE]"
		}
		fmt.Printf("  %s | %s | %s | age: %s%s\n",
			a.ID[:8], a.Tool, a.BlastRadius, formatAge(age), stale)
	}
	fmt.Println("\nUse `kai confirm <id>` or `kai reject <id>` to resolve.")
}

func runWhy(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: kai why <run-id>")
		os.Exit(1)
	}
	runID := args[0]

	cfg := mustLoadConfig()
	db := mustOpenDB(cfg)
	defer db.Close()

	actions, err := db.ActionsForRun(runID)
	if err != nil {
		die("querying actions: %v", err)
	}
	if len(actions) == 0 {
		fmt.Printf("No actions found for run %s.\n", runID)
		return
	}

	for _, a := range actions {
		ts := time.Unix(a.TS, 0).Format("2006-01-02 15:04:05")
		output := "(none)"
		if a.Output != nil {
			output = truncate(*a.Output, 200)
		}
		confirmed := "auto"
		if a.Confirmed == 1 {
			confirmed = "confirmed"
		}
		fmt.Printf("[%s] [%s] %s  params=%s  output=%s\n",
			a.BlastRadius, confirmed, ts, truncate(a.Params, 100), output)
	}
}

// --- Helpers ---

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "kai: "+format+"\n", args...)
	os.Exit(1)
}

func parseCronFromTime(t string) string {
	// Simple parser for "9:00 AM" or "09:30"
	t = strings.ToLower(strings.TrimSpace(t))
	isPM := strings.Contains(t, "pm")
	t = strings.ReplaceAll(t, "am", "")
	t = strings.ReplaceAll(t, "pm", "")
	t = strings.TrimSpace(t)
	parts := strings.Split(t, ":")
	if len(parts) < 2 {
		return "0 9 * * *"
	}
	h, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	if isPM && h != 12 {
		h += 12
	}
	return fmt.Sprintf("%d %d * * *", m, h)
}

func formatAge(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

const (
	resetColor  = "\033[0m"
	greenColor  = "\033[32m"
	yellowColor = "\033[33m"
	redColor    = "\033[31m"
)

func blastRadiusColor(br string) string {
	switch br {
	case "READ_ONLY":
		return greenColor
	case "IDEMPOTENT_WRITE":
		return greenColor
	case "STATE_CHANGE":
		return yellowColor
	case "DESTRUCTIVE":
		return redColor
	default:
		return ""
	}
}

func runReplay(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: kai replay <run-id>")
		os.Exit(1)
	}
	runID := args[0]

	cfg := mustLoadConfig()
	db := mustOpenDB(cfg)
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	result, err := core.Replay(ctx, cfg, db, runID)
	if err != nil {
		die("replay failed: %v", err)
	}
	fmt.Println(result)
}
