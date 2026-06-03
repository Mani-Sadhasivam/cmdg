# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when
working with code in this repository.

## Project Overview

cmdg is a command-line Gmail client written in Go. It uses Google APIs
(Gmail, Drive, People/Contacts) via OAuth2 for a terminal-based email
experience with emacs-like keybindings.

## Build & Development Commands

```bash
# Build the main binary
go build ./cmd/cmdg

# Build with embedded OAuth credentials (used in releases)
go build -o cmdg \
    -ldflags '-X main.InitID=<CLIENT_ID> -X main.InitSecret=<CLIENT_SECRET>' \
    ./cmd/cmdg

# Run all tests
go test ./...

# Run a single test
go test ./cmd/cmdg -run TestCompose

# Lint (uses golangci-lint v2 with config in .golangci.yml)
golangci-lint run

# Vet
go vet ./...
```

## Architecture

The codebase is split between `cmd/cmdg/` (UI and application logic)
and `pkg/` (reusable libraries):

- **`cmd/cmdg/`** — Main application entry point and all UI views
  - `cmdg.go` — Entry point, flag parsing, OAuth setup
  - `view_messagelist.go` — Inbox/label message list view
  - `view_openmessage.go` — Single message reader view
  - `view_attachments.go` — Attachment handling view
  - `compose.go` / `reply.go` — Email composition and reply logic
  - `draft.go` — Draft management

- **`pkg/cmdg/`** — Core Gmail/Google API integration layer
  - `connection.go` — API client initialization (Gmail, Drive,
    People services)
  - `message.go` — Message fetching, parsing, MIME handling
  - `contacts.go` — Google Contacts for address auto-completion
  - `configure.go` — OAuth2 token configuration flow
  - `page.go` — Paginated message list fetching
  - `smime.go` — S/MIME signing support

- **`pkg/display/`** — Terminal screen rendering (ANSI escape
  sequences)
- **`pkg/input/`** — Raw terminal keyboard input handling
- **`pkg/dialog/`** — Interactive dialog components (menus,
  multi-select)
- **`pkg/gpg/`** — GnuPG integration for email signing/encryption

## Key Design Patterns

- The UI uses a view-based architecture where each screen (message
  list, message view, compose) is a self-contained view function
  that takes over the terminal.
- External tools are invoked for editing (`$VISUAL`/`$EDITOR`),
  viewing (`$PAGER`), and HTML rendering (`lynx`).
- Configuration lives in `~/.cmdg/cmdg.conf`; app data (signatures)
  is stored in Google Drive appdata.
- The `pkg/cmdg` package is the API abstraction layer — views in
  `cmd/cmdg/` should use it rather than calling Google APIs directly.
