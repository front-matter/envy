# envy

Environment variable manager. Manages `.env` files via a structured
`env.yaml` manifest — with documentation, validation, and secret auditing built in. Ideal for Docker Compose configurations and 12-factor apps.

## Install

```bash
go install github.com/front-matter/envy@latest
```

Or download a binary from [Releases](https://github.com/front-matter/envy/releases).

## Usage

```
envy [command] [flags]

Commands:
  generate    Generate a .env file from env.yaml
  validate    Validate a .env file against env.yaml
  diff        Show variables missing from or extra in a .env file
  secrets     List or audit secret environment variables
  docs        Generate environment variable documentation
  completion  Generate shell completion scripts

Global flags:
  -m, --manifest string   Path to env.yaml (auto-detected if not given)
```

### Workflow

```bash
# Initial setup — generate a safe template to commit
envy generate --no-secrets > .env.example

# Create your local .env from the template
cp .env.example .env
# ... fill in secrets ...

# Validate before deploying
envy validate --env-file .env.prod

# See what's missing or undocumented
envy diff --env-file .env.prod

# List all secret variables
envy secrets

# Scan git-tracked files for exposed secrets
envy secrets --check

# Generate reference documentation
envy docs -o docs/ENV.md
```

## env.yaml format

`envy` reads a single `env.yaml` manifest as its source of truth:

```yaml
meta:
  name: InvenioRDM Starter
  description: Environment configuration
  invenio_version: "v13.0.8"
  docs: https://inveniordm.docs.cern.ch/reference/configuration/

groups:
  - name: Application
    description: Core Flask/InvenioRDM settings
    vars:
      - key: INVENIO_SECRET_KEY
        secret: true
        required: true
        type: string
        min_length: 32
        description: >
          Flask secret key.
          Generate with: python -c "import secrets; print(secrets.token_hex(32))"

      - key: INVENIO_DEBUG
        default: "False"
        required: false
        type: bool
        allowed: ["True", "False"]
        description: Never True in production.
```

### Var fields

| Field | Type | Description |
|---|---|---|
| `key` | string | Environment variable name |
| `description` | string | Human-readable description |
| `required` | bool | Fail validation if missing |
| `secret` | bool | Omit from `.env.example`, flag in git audit |
| `default` | string | Default value for generated `.env` |
| `type` | string | `string`, `bool`, `int`, `url`, `python_literal` |
| `allowed` | []string | Restrict to enumerated values |
| `min_length` | int | Minimum character length |
| `example` | string | Example value shown in comments |

## Integration with SOPS

Secrets stay encrypted in git via [SOPS](https://github.com/getsops/sops):

```bash
# Audit: list what needs to be in SOPS
envy secrets

# Check nothing leaked into git
envy secrets --check

# Decrypt and validate at deploy time
sops -d secrets.enc.yaml >> .env.prod
envy validate --env-file .env.prod
```

## Build

```bash
make build      # bin/envy
make install    # installs to $GOPATH/bin
make cross      # all platforms → dist/
make test
make completions
```

## Shell completion

```bash
# bash
echo 'source <(envy completion bash)' >> ~/.bashrc

# zsh
echo 'source <(envy completion zsh)' >> ~/.zshrc

# fish
envy completion fish > ~/.config/fish/completions/envy.fish
```
