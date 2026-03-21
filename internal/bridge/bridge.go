package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type PythonBridge struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
}

func New(dbPath string) (*PythonBridge, error) {
	exeDir, err := os.Executable()
	if err != nil {
		exeDir = "."
	} else {
		exeDir = filepath.Dir(exeDir)
	}

	projectRoot := findProjectRoot(exeDir)
	pythonPath := filepath.Join(projectRoot, "python", ".venv", "bin", "python")
	scriptPath := filepath.Join(projectRoot, "python", "main.py")

	cmd := exec.Command(pythonPath, scriptPath, dbPath)
	cmd.Stderr =  nil // todo maybe move this to a log file

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
