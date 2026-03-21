# Semantic File Search

## Overview

This project will be a semantic file search application that can be used on the command line.
The goal is to be able to find a file based on description instead of file names, and additionally other metadata like date.
Users can add directories to be tracked. When a new directory is added, the entire contents (recursively) needs to be scanned, chunked, and indexed. Afterwards, only new file writes will be indexed.

Many different file types will be supported. .txt, and other files that can be parsed as txt (like xml based formats like docx, etc). A future goal is to add OCR for pdfs and other more advanced filetypes.

The UX will be a program running in the terminal with a simple TUI. Everything will be locally hosted, no internet connection or external services needed.

## Architecture

Python for embeddings and semantic search. TBD on libraries, free and local
Go for file operations and parsing spawn python child process when application is run
(python just handles plaintext, no python file parsing)
Go TUI in charm/bubbletea
Go daemon to detect new file writes in tracked folder. Similar to journaling where they aren't written right away (update embeddings/indexing upon starting the application)
SQLlite for local vector database. DB will eventually support temporal metadata

## Development Steps

1. Set up files and database. Both the Go and Pythons sides need to be able to interact with the database. Python for semantic search operations and Go to be able to see which files are indexed and other metadata
2. Get embeddings/basic search working simply with just Python and text input
3. Get a basic Go tui working and connected to python app
4. Add Go file system management with tracked directories.
5. Add daemon for file updates
6. More advanced features: more file types, temporal search

