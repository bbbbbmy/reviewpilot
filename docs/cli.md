# CLI

`reviewpilot` ships as a single binary that doubles as the server
entry point and a one-shot local diff reviewer.

```bash
$ reviewpilot --help
reviewpilot — Self-hosted AI code review bot

Usage:
  reviewpilot [command]

Available Commands:
  completion  Generate the autocompletion script
  help        Help about any command
  review      Run a review on a git range or patch from stdin
  server      Run the HTTP server (default if no subcommand given)
  version     Print the build version

Flags:
  -h, --help   help for reviewpilot

Use "reviewpilot [command] --help" for more information about a command.
```

## `reviewpilot version`

Prints the build version (overridable at link time with
`-ldflags "-X main.version=v0.5.0"`).

```bash
$ reviewpilot version
v0.4.0
```

## `reviewpilot server`

Run the HTTP server. This is the default — running `reviewpilot` with
no subcommand is equivalent to `reviewpilot server`.

See [deployment.md](deployment.md) for production setup. For local dev:

```bash
export RP_LLM_OPENAI_API_KEY="sk-..."
export RP_GITHUB_WEBHOOK_SECRET="shh"
export RP_GITHUB_TOKEN="ghp_..."
export RP_ADMIN_USER=admin
export RP_ADMIN_PASS=devpass
./reviewpilot server
# {"time":"...","level":"INFO","msg":"reviewpilot server starting","version":"v0.4.0-dev","addr":":8080"}
```

`RP_DEV_MODE=1` skips Basic Auth for development.

## `reviewpilot review`

Run a review on a git range or a patch from stdin. Useful in CI before
pushing or as a quick local check.

### Flags

```
$ reviewpilot review --help
Run a review on a git range or patch from stdin

Usage:
  reviewpilot review [<base>...<head>] [flags]

Flags:
  -h, --help              help for review
  -o, --out string        Output destination ('-' for stdout, or a file path) (default "-")
  -r, --repo string       Repository identifier (e.g. bbbbbmy/reviewpilot)
      --stdin             Read the patch from stdin instead of a git range
```

`--repo` is **required**. `--out` defaults to `-` (stdout). `--stdin`
mutually excludes the positional `<base>...<head>` range.

### Examples

Review a local git range:

```bash
./reviewpilot review main..feature --repo bbbbbmy/reviewpilot
```

Review an arbitrary diff:

```bash
git diff main..feature | ./reviewpilot review --stdin --repo bbbbbmy/reviewpilot
```

Save to a file:

```bash
./reviewpilot review main..feature --repo bbbbbmy/reviewpilot --out review.json
```

Pipe to `jq` for human-readable inspection:

```bash
./reviewpilot review main..feature --repo bbbbbmy/reviewpilot | jq '.comments[] | select(.line_number > 0)'
```

### Output schema

```json
{
  "status": "done",
  "reason": "",
  "comments": [
    {
      "file_path": "internal/x.go",
      "line_number": 42,
      "body": "this nil-check is redundant after the early return",
      "fingerprint": "abc1234567890def",
      "posted_to_platform": false,
      "platform_comment_id": ""
    }
  ],
  "tokens_in": 200,
  "tokens_out": 30
}
```

| Field | Type | Description |
|---|---|---|
| `status` | `done` / `failed` / `skipped` | High-level outcome. Non-`done` → non-zero exit code |
| `reason` | string | Why we landed in `failed` / `skipped` (empty for `done`) |
| `comments` | array | Inline comments produced by the LLM (deduplicated locally) |
| `comments[].file_path` | string | File the comment targets (`<pr>` if the comment is whole-PR) |
| `comments[].line_number` | int | Line number (0 for file-level / summary comments) |
| `comments[].body` | string | The comment text |
| `comments[].fingerprint` | string | Stable hash for dedup; matches `comments[].fingerprint` in the GitHub API |
| `tokens_in` / `tokens_out` | int | LLM token usage for this run |

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success (`status: "done"`) |
| 2 | Missing `--repo` |
| 3 | `git diff` failed |
| 4 | Empty diff (nothing to review) |
| 5 | `RP_LLM_OPENAI_API_KEY` missing |
| 6 | Engine construction failed (rare — invalid config) |
| 7 | Engine runtime failed |
| 8 | Output write failed |
| 9 | Non-`done` status (review was `failed` / `skipped`) |

All errors print a structured `{"error": "..."}` JSON to stderr.

### What CLI mode does NOT do

- **Does not post** to GitHub. CLI mode passes `Platform: nil` to the
  engine, so dedup against existing GitHub comments is skipped. If you
  want to drive GitHub comments, run `reviewpilot server` and send
  webhooks.
- **Does not persist** to SQLite. The CLI's feedback store is in-memory
  only — feedback submitted via the dashboard while CLI is running
  will not be visible to the next CLI invocation in the same process.
  For persistent feedback, use the server.

### CI usage

```yaml
# .github/workflows/reviewpilot.yml
name: reviewpilot
on: [pull_request]
jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - name: Install reviewpilot
        run: |
          curl -L https://github.com/bbbbbmy/reviewpilot/releases/download/v0.4.0/reviewpilot_linux_amd64.tar.gz | tar xz
      - name: Run review
        env:
          RP_LLM_OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
        run: |
          ./reviewpilot review origin/${{ github.base_ref }}...origin/${{ github.head_ref }} \
            --repo ${{ github.repository }} --out review.json
      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: reviewpilot
          path: review.json
```

Note: this is *local CI feedback* — it doesn't post back to GitHub as
a comment. To do that, you'd need a GitHub App installed in your org
that calls a server-mode `reviewpilot`.

## Shell completion

```bash
reviewpilot completion bash > /etc/bash_completion.d/reviewpilot
reviewpilot completion zsh > "${fpath[1]}/_reviewpilot"
```