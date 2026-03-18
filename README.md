[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/front-matter/envy.svg)](https://pkg.go.dev/github.com/front-matter/envy)
[![Go Report Card](https://goreportcard.com/badge/github.com/front-matter/envy)](https://goreportcard.com/report/github.com/front-matter/envy)

# envy

Envy is an environment variable manager for Docker. It manages `.env` and `compose.yaml` files via a structured
`env.yaml` manifest — with structure, documentation, validation, and secret auditing built in.
All env variables are defined in the env.yaml manifest, which generates .env files and compose.yaml files as needed. 

All env variables are always treated as strings to avoid type casting issues with Docker and environment variables in general. 

## Install

Envy is a single Go binary, available for download from the [releases page](https://github.com/front-matter/envy/releases). Download the binary for your platform (Linux, Mac, Windows; X86 or ARM architecture), and place it in your PATH. Linux packages in deb, rpm and apk formats are also available from the releases page.

### Homebrew

To install envy on macOS using [Homebrew](https://brew.sh/), run:

```bash
brew tap front-matter/envy
brew install envy
```

### Go

To install envy using Go, run:


```bash
go install github.com/front-matter/envy@latest
```

Or download a binary from [Releases](https://github.com/front-matter/envy/releases).

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
  import      Import .env and/or compose.yaml and generate env.yaml
  lint        Lint env.yaml for warnings
  diff        Show variables missing from or extra in a .env or
              compose.yaml file
  validate    Validate an .env or compose.yaml file against env.yaml
  compose     Generate a Docker Compose file from env.yaml
  generate    Generate a .env file from env.yaml
  secrets     List or audit secret environment variables
  docs        Generate environment variable documentation
  completion  Generate shell completion scripts

Global flags:
  -m, --manifest string   Path to env.yaml (auto-detected if not given)
```

### Workflow

```bash
# Import env and compose files into env.yaml
# Auto-detects one env file: .env preferred over .env.example
# Auto-detects one compose file in this order: compose.yaml, compose.yml, docker-compose.yaml, docker-compose.yml
envy import

# Or import a specific file/directory via positional arg
envy import .env
envy import compose.yaml
envy import compose.sops.yaml
envy import ./config

# --file accepts a folder and writes ./generated/env.yaml
envy import compose.yaml --file ./generated

# Safety: if target file already exists, import prints a warning and does not overwrite it

# Lint manifest for non-fatal issues (e.g. unknown service groups)
envy lint

# See what's missing or undocumented
envy diff .env.prod

# Generate Docker Compose from env defaults
envy compose

# Generate Docker Compose for Coolify
envy compose --flavor coolify

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

# Generate reference documentation
envy docs -o docs/ENV.md
```

If the imported compose file contains a top-level `sops` block, `envy import` automatically decrypts it before parsing.

## env.yaml format

`envy` reads a single `env.yaml` manifest as its source of truth:

```yaml
meta:
  name: InvenioRDM Starter
  description: Environment configuration
  version: "v13.0.8.1"
  docs: https://inveniordm.docs.cern.ch/reference/configuration/

services:
  web:
    image: ghcr.io/front-matter/invenio-rdm-starter:latest
    platform: linux/amd64
    entrypoint: ["/entrypoint.sh"]
    command: ["celery", "-A", "invenio_app.celery", "worker", "--loglevel=INFO"]
    description: Frontend/API service
    groups: [application, database]

groups:
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

`groups` is a map keyed by a stable slug (for example `application`, `database`).
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
| `example` | Example value shown in comments |

## Installation

Envy is a single Go binary, available for download from the [releases page](https://github.com/front-matter/envy/releases). Download the binary for your platform (Linux, Mac, Windows; X86 or ARM architecture), and place it in your PATH. Linux packages in deb, rpm and apk formats are also available from the releases page.

### Homebrew

To install envy on macOS using [Homebrew](https://brew.sh/), run:

```bash
brew tap front-matter/envy
brew install envy
```

### Go

To install envy using Go, run:

```bash
go install github.com/front-matter/envy@latest
```


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

## Integration with SOPS

Secrets stay encrypted in git via [SOPS](https://github.com/getsops/sops):

```bash
# Audit: list what needs to be in SOPS
envy secrets

# Import an encrypted compose file directly
envy import compose.sops.yaml

# Check nothing leaked into git
envy secrets --check

# Decrypt and validate at deploy time
sops -d secrets.enc.yaml >> .env.prod
envy validate .env.prod
```

## Meta

Please note that this project is released with a [Contributor Code of Conduct](https://github.com/front-matter/envy/blob/main/CODE_OF_CONDUCT.md). By participating in this project you agree to abide by its terms.

License: [MIT](https://github.com/front-matter/envy/blob/main/LICENSE)

