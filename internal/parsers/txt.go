package parsers

// Parser for plaintext file types (.txt, .csv, .log)

import (
	"fmt"
	"os"
	"strings"
	"path/filepath"
	"unicode"
)

const txtMaxChars = 500

type PlaintextParser struct{}

func (t *PlaintextParser) Extensions() []string {
	return []string{".txt", ".csv", ".log"} 
}

func cleanText (text string, ext string) string {
	var b strings.Builder
	for _, r := range text {
		if (unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) || unicode.IsPunct(r)) {
			b.WriteRune(r)
		}
	}

	return b.String()
}

func (t *PlaintextParser) Parse(fpath string) ([]string, error) {
	content, err := os.ReadFile(fpath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	text := filepath.Base(fpath) + " " + string(content)
	cleanedText := cleanText(text, filepath.Ext(fpath))

	if strings.TrimSpace(cleanedText) == "" {
		return nil, nil
	}

	return ChunkText(cleanedText, txtMaxChars), nil
}
