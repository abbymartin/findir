package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"semantic-files/internal/bridge"
	"semantic-files/internal/db"
	"semantic-files/internal/tui"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("getting home directory: %v", err)
	}
	dataDir := filepath.Join(home, ".local", "share", "semantic-files")
	dbPath := filepath.Join(dataDir, "semantic_files.db")

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("creating data directory: %v", err)
	}

	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("initializing database: %v", err)
	}
	defer database.Close()

	b, err := bridge.New(dbPath)
	if err != nil {
		log.Fatalf("starting python bridge: %v", err)
	}
	defer b.Close()

	// launch tui
	model := tui.New(b)
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
