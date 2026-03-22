package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"semantic-files/internal/bridge"
	"semantic-files/internal/db"
	"semantic-files/internal/parsers"
)

type Indexer struct {
	DB       *db.DB
	Bridge   *bridge.PythonBridge
	registry *parsers.Registry
}

func New(database *db.DB, b *bridge.PythonBridge) *Indexer {
	registry := parsers.NewRegistry(
		&parsers.PlaintextParser{},
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
		fmt.Fprintf(os.Stderr, "Removed child directory %s (now covered by %s)\n", child.Path, absPath)
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

	fmt.Fprintf(os.Stderr, "Indexed: %s (%d chunks)\n", path, len(chunks))
	return nil
}

// todo rework this entirely!
func (idx *Indexer) IndexNewFiles() error {
	dirs, err := idx.DB.GetTrackedDirectories()
	if err != nil {
		return fmt.Errorf("getting tracked directories: %w", err)
	}

	for _, dir := range dirs {
		if err := idx.ScanDirectory(dir.Path, dir.ID); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: error scanning %s: %v\n", dir.Path, err)
		}
	}
	return nil
}
