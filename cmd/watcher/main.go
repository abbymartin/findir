package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"findir/internal/db"
)

var supportedExts = map[string]bool{
	".txt": true,
	".csv": true,
	".log": true,
	".md":  true,
}

type watcher struct {
	fd          int
	watches     map[int]string // wd -> directory path
	journalPath string
	pidPath     string
}

type journalEntry struct {
	Path      string `json:"path"`
	Event     string `json:"event"`
	Timestamp int64  `json:"timestamp"`
}

func newWatcher(journalPath, pidPath string) (*watcher, error) {
	fd, err := syscall.InotifyInit()
	if err != nil {
		return nil, fmt.Errorf("inotify_init: %w", err)
	}
	return &watcher{
		fd:          fd,
		watches:     make(map[int]string),
		journalPath: journalPath,
		pidPath:     pidPath,
	}, nil
}

func (w *watcher) addWatch(path string) error {
	wd, err := syscall.InotifyAddWatch(w.fd, path,
		syscall.IN_CLOSE_WRITE|syscall.IN_CREATE|syscall.IN_MOVED_TO)
	if err != nil {
		return fmt.Errorf("inotify_add_watch %s: %w", path, err)
	}
	w.watches[wd] = path
	return nil
}

func (w *watcher) addWatchRecursive(path string) error {
	if err := w.addWatch(path); err != nil {
		return err
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil // skip dirs we can't read
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		w.addWatchRecursive(filepath.Join(path, entry.Name()))
	}
	return nil
}

func (w *watcher) writeJournalEntry(filePath, eventType string) {
	f, err := os.OpenFile(w.journalPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("watcher: cannot open journal: %v", err)
		return
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		log.Printf("watcher: flock failed: %v", err)
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	entry := journalEntry{
		Path:      filePath,
		Event:     eventType,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(entry)
	f.Write(data)
	f.Write([]byte("\n"))
}

func (w *watcher) cleanup() {
	for wd := range w.watches {
		syscall.InotifyRmWatch(w.fd, uint32(wd))
	}
	syscall.Close(w.fd)
	os.Remove(w.pidPath)
}

func (w *watcher) run() {
	buf := make([]byte, 64*1024)

	for {
		n, err := syscall.Read(w.fd, buf)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return
		}

		offset := 0
		for offset < n {
			event := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[offset]))
			nameLen := int(event.Len)
			offset += syscall.SizeofInotifyEvent + nameLen

			if nameLen == 0 {
				continue
			}

			// Extract filename (null-terminated within the Len bytes)
			nameBytes := buf[offset-nameLen : offset]
			name := string(nameBytes[:clen(nameBytes)])

			dirPath, ok := w.watches[int(event.Wd)]
			if !ok {
				continue
			}

			fullPath := filepath.Join(dirPath, name)

			if event.Mask&syscall.IN_ISDIR != 0 {
				if event.Mask&(syscall.IN_CREATE|syscall.IN_MOVED_TO) != 0 {
					w.addWatchRecursive(fullPath)
				}
				continue
			}

			ext := strings.ToLower(filepath.Ext(name))
			if !supportedExts[ext] {
				continue
			}

			eventType := "write"
			if event.Mask&syscall.IN_CREATE != 0 {
				eventType = "create"
			} else if event.Mask&syscall.IN_MOVED_TO != 0 {
				eventType = "move"
			}

			w.writeJournalEntry(fullPath, eventType)
		}
	}
}

// clen returns the index of the first null byte (C string length)
func clen(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return len(b)
}

func daemonize() error {
	// Re-exec ourselves with a marker env var
	if os.Getenv("_WATCHER_DAEMON") == "1" {
		return nil // already daemonized
	}

	args := os.Args
	env := append(os.Environ(), "_WATCHER_DAEMON=1")

	attr := &os.ProcAttr{
		Dir:   "/",
		Env:   env,
		Files: []*os.File{nil, nil, nil}, // close stdin/stdout/stderr
	}

	proc, err := os.StartProcess(args[0], args, attr)
	if err != nil {
		return fmt.Errorf("daemonize: %w", err)
	}
	proc.Release()
	os.Exit(0)
	return nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <db_path> <journal_path>\n", os.Args[0])
		os.Exit(1)
	}

	dbPath := os.Args[1]
	journalPath := os.Args[2]
	pidPath := filepath.Join(filepath.Dir(journalPath), "daemon.pid")

	// Open DB to read tracked directories
	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}

	dirs, err := database.GetTrackedDirectories()
	if err != nil {
		database.Close()
		log.Fatalf("reading tracked directories: %v", err)
	}
	database.Close()

	if len(dirs) == 0 {
		log.Fatalf("no tracked directories found")
	}

	// Set up inotify watches before daemonizing so errors are visible
	w, err := newWatcher(journalPath, pidPath)
	if err != nil {
		log.Fatalf("creating watcher: %v", err)
	}

	for _, dir := range dirs {
		fmt.Fprintf(os.Stderr, "watcher: watching %s\n", dir.Path)
		if err := w.addWatchRecursive(dir.Path); err != nil {
			log.Printf("warning: %v", err)
		}
	}

	fmt.Fprintf(os.Stderr, "watcher: watching %d directories, daemonizing...\n", len(w.watches))

	// Daemonize
	if err := daemonize(); err != nil {
		log.Fatalf("daemonize: %v", err)
	}

	// Write PID file
	os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigCh
		w.cleanup()
		os.Exit(0)
	}()

	w.run()
	w.cleanup()
}
