package parsers

import "strings"

// split text into chunks no larger than maxChars
// splits on paragraph boundaries (double newlines), merges short paragraphs together.
func ChunkText(text string, maxChars int) []string {
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
