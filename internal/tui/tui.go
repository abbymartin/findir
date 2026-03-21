package tui

import (
	"fmt"
	"strings"

	"semantic-files/internal/bridge"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SearchResult struct {
	FilePath  string
	ChunkText string
	Score     float64
}

type searchResultsMsg struct {
	results []SearchResult
}

type searchErrorMsg struct {
	err error
}

type Model struct {
	bridge    *bridge.PythonBridge
	textInput textinput.Model
	results   []SearchResult
	loading   bool
	err       error
	searched  bool
}

func New(b *bridge.PythonBridge) Model {
	ti := textinput.New()
	ti.Placeholder = "describe the file you're looking for..."
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 60

	return Model{
		bridge:    b,
		textInput: ti,
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			query := strings.TrimSpace(m.textInput.Value())
			if query == "" {
				return m, nil
			}
			m.loading = true
			m.searched = true
			m.err = nil
			return m, m.doSearch(query)
		}

	case searchResultsMsg:
		m.loading = false
		m.results = msg.results
		return m, nil

	case searchErrorMsg:
		m.loading = false
		m.err = msg.err
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) doSearch(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.bridge.Send(map[string]interface{}{
			"action": "search",
			"query":  query,
			"top_k":  10,
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
				Score:     0,
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
)

func (m Model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Semantic File Search"))
	b.WriteString("\n")

	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")

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

	b.WriteString(statusStyle.Render("enter: search • esc: quit"))
	b.WriteString("\n")

	return b.String()
}
