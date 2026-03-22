package parsers

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type MarkdownParser struct{}

func (m *MarkdownParser) Extensions() []string {
	return []string{".md"}
}

func (m *MarkdownParser) Parse(fpath string) ([]string, error) {
	content, err := os.ReadFile(fpath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	plaintext := extractText(content)
	fullText := filepath.Base(fpath) + " " + plaintext
	cleaned := cleanText(fullText, ".md")

	if strings.TrimSpace(cleaned) == "" {
		return nil, nil
	}

	return ChunkText(cleaned, txtMaxChars), nil
}

func extractText(source []byte) string {
	md := goldmark.New()
	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)

	var buf bytes.Buffer
	walkNode(doc, source, &buf)
	return buf.String()
}

func walkNode(n ast.Node, source []byte, buf *bytes.Buffer) {
	switch n.Kind() {
	case ast.KindHTMLBlock, ast.KindRawHTML:
		return
	case ast.KindImage:
		return
	case ast.KindCodeSpan, ast.KindFencedCodeBlock, ast.KindCodeBlock:
		// Include code content as-is for searchability
	}

	if n.Type() == ast.TypeBlock && buf.Len() > 0 {
		// Add paragraph separation for block elements
		last := buf.Bytes()[buf.Len()-1]
		if last != '\n' {
			buf.WriteByte('\n')
		}
		if n.Kind() == ast.KindParagraph || n.Kind() == ast.KindHeading {
			buf.WriteByte('\n')
		}
	}

	// Write text content from leaf nodes
	if n.HasChildren() {
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			walkNode(child, source, buf)
		}
	} else {
		// Text node — extract segments
		if n.Kind() == ast.KindText {
			textNode := n.(*ast.Text)
			buf.Write(textNode.Segment.Value(source))
			if textNode.SoftLineBreak() || textNode.HardLineBreak() {
				buf.WriteByte('\n')
			} else {
				buf.WriteByte(' ')
			}
		} else if n.Kind() == ast.KindCodeSpan {
			// handled by children
		} else {
			// For other leaf nodes (code blocks, etc), extract lines
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				line := lines.At(i)
				buf.Write(line.Value(source))
			}
		}
	}
}
