# Findir

Findir is a terminal application for Linux that performs semantic search across your filesystem. Everything runs locally with minimal overhead; no external services needed.

## Setup

### Prerequisites
  - Go 1.25+
  - Python 3.10+
  - poppler-utils for PDF parsing: `sudo apt install poppler-utils`

### Installation

1. Clone the repo  
  `git clone https://github.com/abbymartin/Findir.git`  
  `cd findir`
2. Set up Python environment  
  `cd python`  
  `python3 -m venv venv`  
  `source venv/bin/activate`  
  `pip install fastembed onnxruntime`  
  `cd ..`
3. Install Go dependencies  
  `go mod download`
4. Build the binaries  
  `go build -o findir ./cmd/findir`  
  `go build -o watcher ./cmd/watcher`

### Usage

#### Launch the TUI
`./findir`

#### List tracked directories:
`./findir --list-dirs`

#### Add a tracked directory:
`./findir --add /path/to/directory`

#### Remove a tracked directories:
`./findir --list-remove /path/to/directory`

#### Start the file watcher daemon (auto re-indexes on file changes):
`./findir --daemon-start`

#### Stop the daemon:
`./findir --daemon-stop`
