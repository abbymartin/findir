package indexer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"findir/internal/bridge"
	"findir/internal/db"
	"findir/internal/parsers"
)

type Indexer struct {
	DB       *db.DB
	Bridge   *bridge.PythonBridge
	Log      func(string)
	registry *parsers.Registry
}

func (idx *Indexer) log(msg string) {
	if idx.Log != nil {
		idx.Log(msg)
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
}

func New(database *db.DB, b *bridge.PythonBridge) *Indexer {
	registry := parsers.NewRegistry(
		&parsers.PlaintextParser{},
		&parsers.MarkdownParser{},
		&parsers.CodeParser{},
		&parsers.MarkupParser{},
		&parsers.LatexParser{},
		&parsers.PdfParser{},
	)
	return &Indexer{DB: database, Bridge: b, registry: registry}
}

func (idx *Indexer) AddAndIndex(dirPath string) error {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("path does not exist: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", absPath)
	}

	// Reject if a parent directory is already tracked
	parent, err := idx.DB.GetParentTrackedDirectory(absPath)
	if err != nil {
		return fmt.Errorf("checking parent directories: %w", err)
	}
	if parent != nil {
		return fmt.Errorf("%s is already covered by tracked directory %s", absPath, parent.Path)
	}

	// Remove any child directories that will be covered by this parent
	children, err := idx.DB.GetChildTrackedDirectories(absPath)
	if err != nil {
		return fmt.Errorf("checking child directories: %w", err)
	}
	for _, child := range children {
		if err := idx.DB.RemoveTrackedDirectory(child.ID); err != nil {
			return fmt.Errorf("removing child directory %s: %w", child.Path, err)
		}
		idx.log(fmt.Sprintf("Removed child directory %s (now covered by %s)", child.Path, absPath))
	}

	dirID, err := idx.DB.AddTrackedDirectory(absPath)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			dir, lookupErr := idx.DB.GetTrackedDirectoryByPath(absPath)
			if lookupErr != nil {
				return fmt.Errorf("directory already tracked but lookup failed: %w", lookupErr)
			}
			dirID = dir.ID
		} else {
			return fmt.Errorf("adding tracked directory: %w", err)
		}
	}

	return idx.ScanDirectory(absPath, dirID)
}

func (idx *Indexer) ScanDirectory(dirPath string, dirID int64) error {
	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip files we can't access
		}

		if info.IsDir() {
			return nil // Walk recurses automatically; all files use parent dirID
		}

		ext := strings.ToLower(filepath.Ext(path))
		if idx.registry.Get(ext) == nil {
			return nil
		}

		indexed, err := idx.DB.FileIsIndexedByPath(path)
		if err != nil {
			return fmt.Errorf("checking if indexed: %w", err)
		}
		if indexed {
			return nil
		}

		return idx.indexFile(path, dirID, info)
	})
}

func (idx *Indexer) indexFile(path string, dirID int64, info os.FileInfo) error {
	ext := strings.ToLower(filepath.Ext(path))
	parser := idx.registry.Get(ext)
	if parser == nil {
		return nil
	}

	chunks, err := parser.Parse(path)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(chunks) == 0 {
		return nil
	}

	fileID, err := idx.DB.InsertIndexedFile(dirID, path, info.Size(), info.ModTime())
	if err != nil {
		return fmt.Errorf("inserting indexed file: %w", err)
	}

	resp, err := idx.Bridge.Send(map[string]interface{}{
		"action":  "index_file",
		"file_id": fileID,
		"chunks":  chunks,
	})
	if err != nil {
		return fmt.Errorf("sending to python: %w", err)
	}

	if errStr, ok := resp["error"].(string); ok {
		return fmt.Errorf("python error: %s", errStr)
	}

	idx.log(fmt.Sprintf("Indexed: %s (%d chunks)", path, len(chunks)))
	return nil
}

type journalEntry struct {
	Path      string `json:"path"`
	Event     string `json:"event"`
	Timestamp int64  `json:"timestamp"`
}

func (idx *Indexer) ProcessJournal(journalPath string) (int, error) {
	f, err := os.OpenFile(journalPath, os.O_RDWR, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("opening journal: %w", err)
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return 0, fmt.Errorf("locking journal: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Collect unique file paths
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry journalEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			idx.log(fmt.Sprintf("Warning: skipping bad journal line: %s", line))
			continue
		}
		seen[entry.Path] = true
	}

	// Truncate the journal
	if err := f.Truncate(0); err != nil {
		return 0, fmt.Errorf("truncating journal: %w", err)
	}

	if len(seen) == 0 {
		return 0, nil
	}

	// Re-index each file
	count := 0
	for filePath := range seen {
		info, err := os.Stat(filePath)
		if err != nil {
			idx.log(fmt.Sprintf("Warning: journal file gone: %s", filePath))
			continue
		}

		dir, err := idx.DB.FindTrackedDirectoryForFile(filePath)
		if err != nil || dir == nil {
			idx.log(fmt.Sprintf("Warning: no tracked directory for %s", filePath))
			continue
		}

		// Delete old index entry (embeddings cascade via FK)
		idx.DB.DeleteIndexedFileByPath(filePath)

		if err := idx.indexFile(filePath, dir.ID, info); err != nil {
			idx.log(fmt.Sprintf("Warning: re-indexing %s: %v", filePath, err))
			continue
		}
		count++
	}

	return count, nil
}