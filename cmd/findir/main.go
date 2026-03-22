package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"findir/internal/bridge"
	"findir/internal/db"
	"findir/internal/indexer"
	"findir/internal/tui"
)

func main() {
	addDir := flag.String("add", "", "add a directory to track and index its files")
	removeDir := flag.String("remove", "", "stop tracking a directory and remove its index")
	listDirs := flag.Bool("list-dirs", false, "list all tracked directories and exit")
	daemonStart := flag.Bool("daemon-start", false, "start the file watcher daemon")
	daemonStop := flag.Bool("daemon-stop", false, "stop the file watcher daemon")
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("getting home directory: %v", err)
	}
	dataDir := filepath.Join(home, ".local", "share", "findir")
	dbPath := filepath.Join(dataDir, "findir.db")

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("creating data directory: %v", err)
	}

	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("initializing database: %v", err)
	}
	defer database.Close()

	if *removeDir != "" {
		dir, err := database.GetTrackedDirectoryByPath(*removeDir)
		if err != nil {
			log.Fatalf("directory not found: %s", *removeDir)
		}
		if err := database.RemoveTrackedDirectory(dir.ID); err != nil {
			log.Fatalf("removing directory: %v", err)
		}
		fmt.Printf("Removed: %s\n", *removeDir)
		return
	}

	if *listDirs {
		dirs, err := database.GetTrackedDirectories()
		if err != nil {
			log.Fatalf("listing directories: %v", err)
		}
		if len(dirs) == 0 {
			fmt.Println("No tracked directories.")
		}
		for _, d := range dirs {
			fmt.Println(d.Path)
		}
		return
	}

	journalPath := filepath.Join(dataDir, "journal.jsonl")
	pidPath := filepath.Join(dataDir, "daemon.pid")

	if *daemonStart {
		exePath, _ := os.Executable()
		watcherPath := filepath.Join(filepath.Dir(exePath), "watcher")
		if _, err := os.Stat(watcherPath); err != nil {
			log.Fatalf("watcher binary not found. Build it with: go build -o watcher ./cmd/watcher")
		}

		pidData, err := os.ReadFile(pidPath)
		if (err == nil) {
			pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
			if err != nil {
				log.Fatalf("invalid PID file")
			}

			log.Fatalf("Daemon already running (PID %d). Use findir --daemon-stop if you want to stop it.", pid)
		}

		cmd := exec.Command(watcherPath, dbPath, journalPath)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("starting daemon: %v", err)
		}
		fmt.Printf("Daemon started. Use findir --daemon-stop to stop it\n")
		return
	}

	if *daemonStop {
		pidData, err := os.ReadFile(pidPath)
		if err != nil {
			log.Fatalf("daemon not running (no PID found)")
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err != nil {
			log.Fatalf("invalid PID file")
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			log.Fatalf("process not found: %v", err)
		}
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			log.Fatalf("sending SIGTERM: %v", err)
		}
		fmt.Printf("Daemon (PID %d) stopped.\n", pid)
		return
	}

	b, err := bridge.New(dbPath)
	if err != nil {
		log.Fatalf("starting python bridge: %v", err)
	}
	defer b.Close()

	idx := indexer.New(database, b)

	if *addDir != "" {
		fmt.Fprintf(os.Stderr, "Adding directory: %s\n", *addDir)
		if err := idx.AddAndIndex(*addDir); err != nil {
			log.Fatalf("adding directory: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Done.\n")
		return
	}

	// Process journal from daemon (re-index changed files)
	if count, err := idx.ProcessJournal(journalPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: processing journal: %v\n", err)
	} else if count > 0 {
		fmt.Fprintf(os.Stderr, "Re-indexed %d files from daemon journal.\n", count)
	}

	// Launch TUI
	model := tui.New(b, database, idx, journalPath)
	p := tea.NewProgram(&model, tea.WithAltScreen())
	model.SetProgram(p)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

