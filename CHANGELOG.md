# Changelog

## Unreleased

- `hibira approvals`: manage Hibira Approvals workspaces with an API token (`workspaces`, `create`, `members`, `add`, `remove`, `test`, `requests`). Needs a Hibira server with the Approvals workspace API.
- `install.sh` (macOS, Linux) and `install.ps1` (Windows): one-line installers that pick the right release archive, verify its checksum and put `hibira` on your PATH.

## 0.1.0

- `hibira tui` (also plain `hibira` in a terminal): interactive view with a channel list, live event stream, event detail pane, and forms to subscribe and publish.
- `hibira trigger`: publish an event to one or more channels through the HTTP API.
- `hibira listen`: subscribe to public, private and presence channels and print events (text or JSON lines), with automatic reconnect.
- `hibira channels`: list occupied channels.
- `hibira config`: save credentials in named profiles (`set`, `show`, `use`, `remove`, `path`).
