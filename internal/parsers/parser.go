package parsers

// Parser parses a file into text chunks ready for embedding.
type Parser interface {
	// Extensions returns the file extensions this parser handles (lowercase, with dot, e.g. ".txt").
	Extensions() []string
	// Parse reads the file at the given path and returns text chunks.
	Parse(path string) ([]string, error)
}

// Registry maps extensions to their parser.
type Registry struct {
	parsers map[string]Parser
}

func NewRegistry(pp ...Parser) *Registry {
	r := &Registry{parsers: make(map[string]Parser)}
	for _, p := range pp {
		for _, ext := range p.Extensions() {
			r.parsers[ext] = p
		}
	}
	return r
}

// Get returns the parser for the given extension, or nil if unsupported.
func (r *Registry) Get(ext string) Parser {
	return r.parsers[ext]
}
