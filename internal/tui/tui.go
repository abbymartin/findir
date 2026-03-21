package tui

import (
	"fmt"
	"strings"

	"semantic-files/internal/bridge"
	"semantic-files/internal/db"
	"semantic-files/internal/indexer"

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

type searchResultsMsg struct {
	results []SearchResult
}

type searchErrorMsg struct {
	err error
}

type indexDoneMsg struct {
	err error
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
	width       int
	height      int
}

func New(b *bridge.PythonBridge, database *db.DB, idx *indexer.Indexer) Model {
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
	}
}

func (m Model) Init() tea.Cmd {
	b := m.bridge
	return func() tea.Msg {
		b.Send(map[string]interface{}{"action": "warmup"})
		return modelReadyMsg{}
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
		}

	case searchResultsMsg:
		m.loading = false
		m.results = msg.results
		return m, nil

	case searchErrorMsg:
		m.loading = false
		m.err = msg.err
		return m, nil

	case indexDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.indexStatus = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.indexStatus = "Directory indexed successfully."
			m.dirInput.SetValue("")
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

func (m Model) handleAddDir() (tea.Model, tea.Cmd) {
	dirPath := strings.TrimSpace(m.dirInput.Value())
	if dirPath == "" {
		return m, nil
	}
	m.loading = true
	m.indexStatus = fmt.Sprintf("Indexing %s...", dirPath)
	idx := m.indexer
	return m, func() tea.Msg {
		err := idx.AddAndIndex(dirPath)
		return indexDoneMsg{err: err}
	}
}

func (m Model) doSearch(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.bridge.Send(map[string]interface{}{
			"action": "search",
			"query":  query,
			"top_k":  4,
		})
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
	addTab := "Add Directory"
	if m.mode == searchView {
		searchTab = tabActiveStyle.Render("[Search]")
		addTab = tabInactiveStyle.Render(" Add Directory ")
	} else {
		searchTab = tabInactiveStyle.Render(" Search ")
		addTab = tabActiveStyle.Render("[Add Directory]")
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
			b.WriteString("\n")
		} else if m.indexStatus != "" {
			b.WriteString(statusStyle.Render(m.indexStatus))
			b.WriteString("\n")
		}
	}

	b.WriteString(statusStyle.Render("tab: switch view • enter: submit • esc: quit"))
	b.WriteString("\n")

	return b.String()
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
			if len(preview) > 120 {
				preview = preview[:120] + "..."
			}
			b.WriteString(resultTextStyle.Render(preview))
			b.WriteString("\n\n")
		}
	}
}
