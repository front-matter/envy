[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/front-matter/envy.svg)](https://pkg.go.dev/github.com/front-matter/envy)
[![Go Report Card](https://goreportcard.com/badge/github.com/front-matter/envy)](https://goreportcard.com/report/github.com/front-matter/envy)

# envy

Envy manages Docker Compose files. It validates and lints them,
manages Compose profiles, audits secrets, generates and diffs .env files, and produces documentation that can be deployed as a static website.

All env variables are always treated as strings to avoid type casting issues with Docker and environment variables in general. 

## Install

Envy is a single Go binary, available for download from the [releases page](https://github.com/front-matter/envy/releases). Download the binary for your platform (Linux, Mac, Windows; X86 or ARM architecture), and place it in your PATH. Linux packages in deb, rpm and apk formats are also available from the releases page.

### Go

To install envy using Go, run:

```bash
go install github.com/front-matter/envy@latest
```

### Homebrew

To install envy on macOS using [Homebrew](https://brew.sh/), run:

```bash
brew tap front-matter/envy
brew install envy
```

## Usage

```
envy [command] [flags]

Commands:
  import      Import .env files and generate compose.yaml
  validate    Validate a compose.yaml (using docker compose config)
  lint        Lint compose.yaml for warnings
  diff        Show variables missing from or extra in a .env file
  generate    Generate a .env file from compose.yaml
  secrets     List or audit secret environment variables
  build       Generate documentation site for compose.yaml file
  server      Run local documentation site generated from compose.yaml
  deploy      Deploy documentation site to GitHub Pages
  completion  Generate shell completion scripts

Global flags:
  -m, --manifest string   Path to compose.yaml (auto-detected if not given)
  -v, --version           version for envy
```

### Workflow

```bash
# Import env files into compose.yaml
# Auto-detects one env file: .env preferred over .env.example
envy import

# Or import a specific file/directory via positional arg
envy import .env
envy import ./config

# --file accepts a folder and writes ./generated/compose.yaml
envy import .env --file ./generated

# Safety: if target file already exists, import prints a warning and does not overwrite it

# Lint compose.yaml and run go-ruleguard checks
envy lint

# See what's missing or undocumented
envy diff .env.prod

# Initial setup — generate a safe template to commit
envy generate --no-secrets > .env.example

# Create your local .env from the template
cp .env.example .env
# ... fill in secrets ...

# Validate before deploying
envy validate .env.prod

# List all secret variables
envy secrets

# Scan git-tracked files for exposed secrets
envy secrets --check

# Print envy version
envy --version

# Build documentation site for compose.yaml
# If docs/index.md exists next to compose.yaml, it is used as the docs home page.
envy build --destination public

# Run local documentation site
envy server --bind 0.0.0.0

# Deploy documentation site to GitHub Pages
envy deploy --target production

## compose.yaml format

`envy` reads a single `compose.yaml` manifest as its source of truth:

```yaml
meta:
  title: InvenioRDM Starter
  description: Environment configuration
  version: "v13.0.8.1"
  docs: https://inveniordm.docs.cern.ch/reference/configuration/
  languageCode: en-US
  ignoreLogs:
    - warning-goldmark-raw-html

services:
  web:
    image: ghcr.io/front-matter/invenio-rdm-starter:latest
    platform: linux/amd64
    entrypoint: ["/entrypoint.sh"]
    command: ["celery", "-A", "invenio_app.celery", "worker", "--loglevel=INFO"]
    description: Frontend/API service
    sets: [application, database]

sets:
  application:
    description: Core Flask/InvenioRDM settings
    vars:
      INVENIO_SECRET_KEY:
        description: >
          Flask secret key.
          Generate with: python -c "import secrets; print(secrets.token_hex(32))"
        default: ""
        secret: true
        required: true

      INVENIO_DEBUG:
        description: Never true in production.
        default: "false"
```

### Var fields

`sets` is a map keyed by a stable slug (for example `application`, `database`).
Top-level `services` reference these slugs to define per-service config sets.
Each service can also define `image` and `platform`. If `platform` is omitted,
compose generation falls back to `linux/amd64`. If `platform` is set, it should
use the form `os/arch` or `os/arch/variant`. `image` should be a Docker image
reference such as `ghcr.io/front-matter/invenio-rdm-starter:latest`. Optional
`entrypoint` and `command` values are string lists and are emitted into compose
when set.

| Field | Description |
|-------|----------------------|
| `key` | Environment variable name |
| `description` | Human-readable description |
| `default` | Default value for generated `.env` |
| `required` | Fail validation if missing |
| `secret` | Omit from `.env.example`, flag in git audit |
| `readonly` | Prevent variable from being exported to generated `.env` when `"true"` (default: `"false"`) |
| `example` | Example value shown in comments |

## Shell Completion

`envy` provides completion scripts for bash, zsh, fish, and PowerShell via the built-in `completion` command.

### zsh

```bash
envy completion zsh > "${fpath[1]}/_envy"
# restart your shell or run: autoload -U compinit && compinit
```

### bash

```bash
envy completion bash > /etc/bash_completion.d/envy
# or for a single user:
envy completion bash >> ~/.bashrc
```

### fish

```bash
envy completion fish > ~/.config/fish/completions/envy.fish
```

### PowerShell

```powershell
envy completion powershell | Out-String | Invoke-Expression
```

## Meta

Please note that this project is released with a [Contributor Code of Conduct](https://github.com/front-matter/envy/blob/main/CODE_OF_CONDUCT.md). By participating in this project you agree to abide by its terms.

License: [MIT](https://github.com/front-matter/envy/blob/main/LICENSE)
