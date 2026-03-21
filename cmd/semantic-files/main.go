package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"semantic-files/internal/db"
)

type PythonBridge struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
}

func NewPythonBridge(dbPath string) (*PythonBridge, error) {
	exeDir, err := os.Executable()
	if err != nil {
		exeDir = "."
	} else {
		exeDir = filepath.Dir(exeDir)
	}

	// Look for python venv relative to project root
	// Walk up from executable to find python/.venv
	projectRoot := findProjectRoot(exeDir)
	pythonPath := filepath.Join(projectRoot, "python", ".venv", "bin", "python")
	scriptPath := filepath.Join(projectRoot, "python", "main.py")

	fmt.Fprintf(os.Stderr, "Python: %s\nScript: %s\nDB: %s\n", pythonPath, scriptPath, dbPath)

	cmd := exec.Command(pythonPath, scriptPath, dbPath)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting python process: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)

	// Wait for ready signal
	if !scanner.Scan() {
		return nil, fmt.Errorf("python process did not send ready signal")
	}

	var ready map[string]string
	if err := json.Unmarshal(scanner.Bytes(), &ready); err != nil {
		return nil, fmt.Errorf("parsing ready signal: %w", err)
	}
	if ready["status"] != "ready" {
		return nil, fmt.Errorf("unexpected ready signal: %s", scanner.Text())
	}

	return &PythonBridge{cmd: cmd, stdin: stdin, stdout: scanner}, nil
}

func (p *PythonBridge) Send(request map[string]interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	if _, err := fmt.Fprintf(p.stdin, "%s\n", data); err != nil {
		return nil, fmt.Errorf("writing to python: %w", err)
	}

	if !p.stdout.Scan() {
		return nil, fmt.Errorf("no response from python")
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(p.stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return resp, nil
}

func (p *PythonBridge) Close() error {
	p.stdin.Close()
	return p.cmd.Wait()
}

func findProjectRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

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

	fmt.Printf("made data dir\n")

	// Initialize database
	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("initializing database: %v", err)
	}
	defer database.Close()

	fmt.Printf("initialzed db\n")

	// Start Python bridge
	bridge, err := NewPythonBridge(dbPath)
	if err != nil {
		log.Fatalf("starting python bridge: %v", err)
	}
	defer bridge.Close()

	fmt.Printf("started python bridge\n")

	// Test with a ping
	resp, err := bridge.Send(map[string]interface{}{"action": "ping"})
	if err != nil {
		log.Fatalf("ping failed: %v", err)
	}

	fmt.Printf("Python bridge connected: %v\n", resp)
	fmt.Println("Semantic File Search initialized successfully.")

	// List tracked directories
	dirs, err := database.GetTrackedDirectories()
	if err != nil {
		log.Fatalf("listing directories: %v", err)
	}
	if len(dirs) == 0 {
		fmt.Println("No tracked directories.")
	} else {
		for _, d := range dirs {
			fmt.Printf("  Tracking: %s\n", d.Path)
		}
	}
}
