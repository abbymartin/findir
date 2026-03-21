package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"semantic-files/internal/bridge"
	"semantic-files/internal/db"
)

type Indexer struct {
	DB     *db.DB
	Bridge *bridge.PythonBridge
}

func New(database *db.DB, b *bridge.PythonBridge) *Indexer {
	return &Indexer{DB: database, Bridge: b}
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
			return nil
		}
		if !isSupportedFile(path) {
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
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	text := string(content)
	if strings.TrimSpace(text) == "" {
		return nil
	}

	chunks := chunkText(text, 500)
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

func isSupportedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".txt"
}

func chunkText(text string, maxChars int) []string {
	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var current string

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		if current != "" && len(current)+len(para)+1 > maxChars {
			chunks = append(chunks, current)
			current = para
		} else if current == "" {
			current = para
		} else {
			current = current + " " + para
		}
	}

	if current != "" {
		chunks = append(chunks, current)
	}

	return chunks
}
