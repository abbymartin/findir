package tui

import (
	"fmt"
	"strings"

	"findir/internal/bridge"
	"findir/internal/db"
	"findir/internal/indexer"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type viewMode int

const (
	searchView viewMode = iota
	addDirView
)

type SearchResult struct {
	FilePath  string
	ChunkText string
	Score     float64
}

type modelReadyMsg struct{}

type trackedDirsMsg struct {
	dirs []db.TrackedDirectory
}

type searchResultsMsg struct {
	results []SearchResult
}

type searchErrorMsg struct {
	err error
}

type indexDoneMsg struct {
	err error
}

type removeDoneMsg struct {
	err error
}

type indexLogMsg struct {
	line string
}

type Model struct {
	bridge      *bridge.PythonBridge
	database    *db.DB
	indexer     *indexer.Indexer
	mode        viewMode
	searchInput textinput.Model
	dirInput    textinput.Model
	results     []SearchResult
	loading     bool
	err         error
	searched    bool
	indexStatus string
	modelReady  bool
	trackedDirs []db.TrackedDirectory
	selectedDir int
	journalPath string
	indexLog []string
	program  *tea.Program
	width    int
	height   int
}

const maxLogLines = 5

func New(b *bridge.PythonBridge, database *db.DB, idx *indexer.Indexer, journalPath string) Model {
	si := textinput.New()
	si.Placeholder = "search..."
	si.CharLimit = 256
	si.Width = 60

	di := textinput.New()
	di.Placeholder = "/path/to/directory"
	di.CharLimit = 512
	di.Width = 60

	return Model{
		bridge:      b,
		database:    database,
		indexer:     idx,
		searchInput: si,
		dirInput:    di,
		journalPath: journalPath,
	}
}

func (m *Model) SetProgram(p *tea.Program) {
	m.program = p
	m.indexer.Log = func(line string) {
		p.Send(indexLogMsg{line: line})
	}
}

func (m Model) Init() tea.Cmd {
	b := m.bridge
	database := m.database
	return tea.Batch(
		func() tea.Msg {
			b.Send(map[string]interface{}{"action": "warmup"})
			return modelReadyMsg{}
		},
		func() tea.Msg {
			dirs, _ := database.GetTrackedDirectories()
			return trackedDirsMsg{dirs: dirs}
		},
	)
}

func loadTrackedDirs(database *db.DB) tea.Cmd {
	return func() tea.Msg {
		dirs, _ := database.GetTrackedDirectories()
		return trackedDirsMsg{dirs: dirs}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case modelReadyMsg:
		m.modelReady = true
		m.searchInput.Focus()
		return m, textinput.Blink

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyTab:
			if !m.modelReady {
				return m, nil
			}
			m.indexLog = nil
			m.indexStatus = ""
			if m.mode == searchView {
				m.mode = addDirView
				m.searchInput.Blur()
				m.dirInput.Focus()
			} else {
				m.mode = searchView
				m.dirInput.Blur()
				m.searchInput.Focus()
			}
			return m, textinput.Blink
		case tea.KeyEnter:
			if !m.modelReady {
				return m, nil
			}
			if m.mode == searchView {
				return m.handleSearch()
			}
			return m.handleAddDir()
		case tea.KeyUp:
			if m.mode == addDirView && len(m.trackedDirs) > 0 {
				m.selectedDir--
				if m.selectedDir < 0 {
					m.selectedDir = 0
				}
			}
			return m, nil
		case tea.KeyDown:
			if m.mode == addDirView && len(m.trackedDirs) > 0 {
				m.selectedDir++
				if m.selectedDir >= len(m.trackedDirs) {
					m.selectedDir = len(m.trackedDirs) - 1
				}
			}
			return m, nil
		}

		// Delete selected directory on 'd' or Delete key in add-dir view
		if m.mode == addDirView && !m.loading && len(m.trackedDirs) > 0 {
			if msg.String() == "ctrl+d" || msg.Type == tea.KeyDelete {
				return m.handleRemoveDir()
			}
		}

	case searchResultsMsg:
		m.loading = false
		m.results = msg.results
		return m, nil

	case searchErrorMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case trackedDirsMsg:
		m.trackedDirs = msg.dirs
		if m.selectedDir >= len(m.trackedDirs) {
			m.selectedDir = len(m.trackedDirs) - 1
		}
		if m.selectedDir < 0 {
			m.selectedDir = 0
		}
		return m, nil

	case indexDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.indexStatus = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.indexStatus = "Directory added successfully."
			m.dirInput.SetValue("")
		}
		return m, loadTrackedDirs(m.database)

	case removeDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.indexStatus = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.indexStatus = "Directory removed."
		}
		return m, loadTrackedDirs(m.database)

	case indexLogMsg:
		m.indexLog = append(m.indexLog, msg.line)
		if len(m.indexLog) > maxLogLines {
			m.indexLog = m.indexLog[len(m.indexLog)-maxLogLines:]
		}
		return m, nil
	}

	if !m.modelReady {
		return m, nil
	}

	var cmd tea.Cmd
	if m.mode == searchView {
		m.searchInput, cmd = m.searchInput.Update(msg)
	} else {
		m.dirInput, cmd = m.dirInput.Update(msg)
	}
	return m, cmd
}

func (m Model) handleSearch() (tea.Model, tea.Cmd) {
	query := strings.TrimSpace(m.searchInput.Value())
	if query == "" {
		return m, nil
	}
	m.loading = true
	m.searched = true
	m.err = nil
	return m, m.doSearch(query)
}

func (m Model) handleRemoveDir() (tea.Model, tea.Cmd) {
	if m.selectedDir < 0 || m.selectedDir >= len(m.trackedDirs) {
		return m, nil
	}
	dir := m.trackedDirs[m.selectedDir]
	m.loading = true
	m.indexLog = nil
	m.indexStatus = fmt.Sprintf("Removing %s...", dir.Path)
	database := m.database
	p := m.program
	return m, func() tea.Msg {
		p.Send(indexLogMsg{line: fmt.Sprintf("Removing: %s", dir.Path)})
		err := database.RemoveTrackedDirectory(dir.ID)
		if err == nil {
			p.Send(indexLogMsg{line: fmt.Sprintf("Removed: %s", dir.Path)})
		}
		return removeDoneMsg{err: err}
	}
}

func (m Model) handleAddDir() (tea.Model, tea.Cmd) {
	dirPath := strings.TrimSpace(m.dirInput.Value())
	if dirPath == "" {
		return m, nil
	}
	m.loading = true
	m.indexLog = nil
	m.indexStatus = fmt.Sprintf("Indexing %s...", dirPath)
	idx := m.indexer
	return m, func() tea.Msg {
		err := idx.AddAndIndex(dirPath)
		return indexDoneMsg{err: err}
	}
}

func parseSearchQuery(input string) (string, map[string]string) {
	filters := make(map[string]string)
	var queryParts []string
	for _, token := range strings.Fields(input) {
		switch {
		case strings.HasPrefix(token, "ext:"):
			filters["ext"] = token[4:]
		case strings.HasPrefix(token, "after:"):
			filters["after"] = token[6:]
		case strings.HasPrefix(token, "before:"):
			filters["before"] = token[7:]
		default:
			queryParts = append(queryParts, token)
		}
	}
	return strings.Join(queryParts, " "), filters
}

func (m Model) doSearch(query string) tea.Cmd {
	return func() tea.Msg {
		// Process any pending journal entries before searching
		m.indexer.ProcessJournal(m.journalPath)

		semanticQuery, filters := parseSearchQuery(query)
		if semanticQuery == "" {
			semanticQuery = query // use full input if no query words remain
		}

		req := map[string]interface{}{
			"action": "search",
			"query":  semanticQuery,
			"top_k":  4,
		}
		if len(filters) > 0 {
			req["filters"] = filters
		}

		resp, err := m.bridge.Send(req)
		if err != nil {
			return searchErrorMsg{err: err}
		}

		if errStr, ok := resp["error"].(string); ok {
			return searchErrorMsg{err: fmt.Errorf("%s", errStr)}
		}

		rawResults, ok := resp["results"].([]interface{})
		if !ok {
			return searchErrorMsg{err: fmt.Errorf("unexpected response format")}
		}

		var results []SearchResult
		for _, r := range rawResults {
			item, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			result := SearchResult{
				ChunkText: fmt.Sprintf("%v", item["chunk_text"]),
			}
			if s, ok := item["score"].(float64); ok {
				result.Score = s
			}
			if fp, ok := item["file_path"].(string); ok {
				result.FilePath = fp
			}
			results = append(results, result)
		}

		return searchResultsMsg{results: results}
	}
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243"))

	resultFileStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))

	resultScoreStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243"))

	resultTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			PaddingLeft(2)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			MarginTop(1)

	loadingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	dirItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			PaddingLeft(2)

	dirLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Bold(true)
)

func (m Model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Semantic File Search"))
	b.WriteString("\n")

	if !m.modelReady {
		b.WriteString("\n")
		b.WriteString(loadingStyle.Render("Loading embedding model..."))
		b.WriteString("\n\n")
		b.WriteString(statusStyle.Render("esc: quit"))
		b.WriteString("\n")
		return b.String()
	}

	// Tab bar
	searchTab := "Search"
	addTab := "Tracked Directories"
	if m.mode == searchView {
		searchTab = tabActiveStyle.Render("[Search]")
		addTab = tabInactiveStyle.Render(" Tracked Directories ")
	} else {
		searchTab = tabInactiveStyle.Render(" Search ")
		addTab = tabActiveStyle.Render("[Tracked Directories]")
	}
	b.WriteString(searchTab + "  " + addTab)
	b.WriteString("\n\n")

	if m.mode == searchView {
		b.WriteString(m.searchInput.View())
		b.WriteString("\n\n")
		m.renderSearchResults(&b)
	} else {
		b.WriteString(m.dirInput.View())
		b.WriteString("\n\n")
		if m.loading {
			b.WriteString(statusStyle.Render(m.indexStatus))
		} else if m.indexStatus != "" {
			b.WriteString(statusStyle.Render(m.indexStatus))
		}
		if len(m.indexLog) > 0 {
			b.WriteString("\n")
			for _, line := range m.indexLog {
				b.WriteString(dirItemStyle.Render(line))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
		m.renderTrackedDirs(&b)
	}

	if m.mode == searchView {
		b.WriteString(statusStyle.Render("filters: type:.py after:YYYY-MM-DD before:YYYY-MM-DD"))
		b.WriteString("\n")
	}
	b.WriteString(statusStyle.Render("tab: switch view • enter: submit • esc: quit"))
	b.WriteString("\n")

	return b.String()
}

func (m Model) renderTrackedDirs(b *strings.Builder) {
	const maxDisplay = 8
	dirs := m.trackedDirs

	if len(dirs) == 0 {
		b.WriteString(statusStyle.Render("No tracked directories yet."))
		b.WriteString("\n")
		return
	}

	b.WriteString(dirLabelStyle.Render("Tracked directories:"))
	b.WriteString("\n")

	display := dirs
	if len(display) > maxDisplay {
		display = dirs[:maxDisplay]
	}
	for i, d := range display {
		if i == m.selectedDir {
			b.WriteString(dirItemStyle.Bold(true).Render("▶ " + d.Path))
		} else {
			b.WriteString(dirItemStyle.Render("  " + d.Path))
		}
		b.WriteString("\n")
	}
	if len(dirs) > maxDisplay {
		b.WriteString(statusStyle.Render(fmt.Sprintf("  ...and %d more. Run `findir --list-dirs` to see all.", len(dirs)-maxDisplay)))
		b.WriteString("\n")
	}
	b.WriteString(statusStyle.Render("↑/↓: select • ctrl+d: remove"))
	b.WriteString("\n")
}

func (m Model) renderSearchResults(b *strings.Builder) {
	if m.loading {
		b.WriteString(statusStyle.Render("Searching..."))
		b.WriteString("\n")
	} else if m.err != nil {
		b.WriteString(statusStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n")
	} else if m.searched && len(m.results) == 0 {
		b.WriteString(statusStyle.Render("No results found."))
		b.WriteString("\n")
	} else if len(m.results) > 0 {
		b.WriteString(statusStyle.Render(fmt.Sprintf("%d results", len(m.results))))
		b.WriteString("\n\n")

		for i, r := range m.results {
			file := r.FilePath
			if file == "" {
				file = "unknown"
			}
			header := fmt.Sprintf("%d. %s  %s",
				i+1,
				resultFileStyle.Render(file),
				resultScoreStyle.Render(fmt.Sprintf("%.4f", r.Score)),
			)
			b.WriteString(header)
			b.WriteString("\n")

			preview := r.ChunkText
			if idx := strings.Index(preview, "\n"); idx != -1 {
				if idx2 := strings.Index(preview[idx+1:], "\n"); idx2 != -1 {
					if idx3 := strings.Index(preview[idx+1+idx2+1:], "\n"); idx3 != -1 {
						preview = preview[:idx+1+idx2+1+idx3] + "..."
					}
				}
			}
			if len(preview) > 120 {
				preview = preview[:120] + "..."
			}
			b.WriteString(resultTextStyle.Render(preview))
			b.WriteString("\n\n")
		}
	}
}
