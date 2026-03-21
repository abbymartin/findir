package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"semantic-files/internal/bridge"
	"semantic-files/internal/db"
	"semantic-files/internal/indexer"
	"semantic-files/internal/tui"
)

func main() {
	addDir := flag.String("add", "", "add a directory to track and index its .txt files")
	flag.Parse()

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

	idx := indexer.New(database, b)

	if *addDir != "" {
		fmt.Fprintf(os.Stderr, "Adding directory: %s\n", *addDir)
		if err := idx.AddAndIndex(*addDir); err != nil {
			log.Fatalf("adding directory: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Done.\n")
		return
	}

	// // Index any new files in tracked directories
	// if err := idx.IndexNewFiles(); err != nil {
	// 	fmt.Fprintf(os.Stderr, "Warning: error indexing new files: %v\n", err)
	// }

	// Launch TUI
	model := tui.New(b, database, idx)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
