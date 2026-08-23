# GitHub Webhook Setup

ReviewPilot receives GitHub events on `POST /webhook`. This guide walks
through registering a GitHub App (recommended) or a personal webhook
(simpler) and pointing it at your deployment.

## Option A: GitHub App (recommended for production)

A GitHub App gets its own installation token per repo, doesn't share
quota with your personal account, and can be installed on multiple repos
at once.

> **Note (v0.4.0)**: The full JWT-exchange flow is **not yet
> implemented** in `internal/platformgithub`. v0.4.0 supports PAT
> tokens via `RP_GITHUB_TOKEN`. The webhook receiver itself works
> with any auth model — the restriction is only on outbound API calls.
> The JWT flow is a planned follow-up; see
> `vibecoding/openspec/changes/` for the relevant proposal.

For now, configure a GitHub App but **use a PAT as the bearer token**:
the webhook secret handles inbound auth; the outbound bearer is a
fine-grained PAT you generate separately. Once the JWT flow ships,
you'll be able to swap `RP_GITHUB_TOKEN` for the auto-rotated
installation token.

### 1. Create the App

1. Visit <https://github.com/settings/apps/new> (or your GHES
   equivalent).
2. Fill in:
   - **GitHub App name**: `ReviewPilot` (or whatever is unique to your org)
   - **Homepage URL**: your dashboard URL
   - **Webhook URL**: `https://reviewpilot.example.com/webhook`
   - **Webhook secret**: generate with `openssl rand -base64 48` and
     save this — it becomes `RP_GITHUB_WEBHOOK_SECRET` on the server.
3. **Repository permissions**:
   - `Contents`: Read (to read PR metadata)
   - `Issues`: Read & Write (to post summary comments)
   - `Pull requests`: Read & Write (to post inline comments)
4. **Subscribe to events**:
   - ☑️ Pull request
5. Save. Note the **App ID** (`RP_GITHUB_APP_ID`) and download the
   **private key** (`.pem`) — that becomes `RP_GITHUB_APP_PRIVATE_KEY`.
6. Generate a fine-grained PAT at
   <https://github.com/settings/tokens?type=beta> with `contents:write`
   on the repos you want to review — this is your `RP_GITHUB_TOKEN`
   for v0.4.0. (Once the JWT flow lands, you can drop this PAT and use
   the App's auto-rotated installation token instead.)

### 2. Install the App

Visit the App's public page → Install. Choose the org / repos you want
to review. The App's installation ID is logged by ReviewPilot when it
sees a webhook (we don't act on it in v0.4.0 yet).

### 3. Configure the server

Set on the server's environment:

```bash
export RP_GITHUB_WEBHOOK_SECRET="<the secret from step 2>"
export RP_GITHUB_TOKEN="ghp_..."           # fine-grained PAT from step 1
export RP_GITHUB_APP_ID="123456"           # for record-keeping; unused in v0.4.0
export RP_GITHUB_APP_PRIVATE_KEY="$(cat path/to/private-key.pem)"  # reserved
```

Restart the server. Watch the logs for `webhook signature verify
failed` — if you see any, the secret doesn't match what GitHub has.

### 4. Test the webhook

Open any PR (or push a commit to an open PR) on an installed repo.
ReviewPilot should:

1. Verify the signature → `POST /webhook` returns 202 Accepted.
2. Run the engine → 1 LLM call.
3. Post an inline comment on the line mentioned in the LLM response.
4. Post a summary comment at the PR level.

The whole run takes ~3–10 seconds depending on the LLM provider.
Refresh `https://github.com/<owner>/<repo>/pull/<n>` to see the
comments.

## Option B: Personal webhook (simpler)

If you just want to try ReviewPilot on one repo and don't want to set
up an App:

1. Go to your repo → Settings → Webhooks → Add webhook.
2. **Payload URL**: `https://reviewpilot.example.com/webhook`
3. **Content type**: `application/json`
4. **Secret**: same as `RP_GITHUB_WEBHOOK_SECRET` above.
5. **Which events**: ☑️ "Let me select individual events" → ☑️ Pull requests
6. Save.

The webhook is owned by your user account, not an App. Use a personal
access token for `RP_GITHUB_TOKEN` (with `repo` scope for private repos).

This setup is fine for personal use but doesn't scale to a team.

## Webhook payload

GitHub sends:

```json
{
  "action": "opened",
  "number": 42,
  "pull_request": {
    "head": { "sha": "deadbeef..." },
    ...
  },
  "repository": { "full_name": "bbbbbmy/reviewpilot" },
  "installation": { "id": 12345 },
  "sender": { "login": "alice" }
}
```

ReviewPilot only acts on actions `opened`, `synchronize`, `reopened`,
`ready_for_review`. All other actions are ignored (HTTP 202 with
`status: ignored` — added in a follow-up).

The PR's diff lives in `pull_request.patch`. For diffs larger than
~65 KiB (GitHub's payload cap), this field is empty — ReviewPilot
gracefully degrades to "no diff to review" (HTTP 202 with
`status: skipped`).

## What ReviewPilot writes back

For each `done` review with N comments, ReviewPilot writes:

- N inline comments via `POST /repos/{owner}/{repo}/pulls/{pr}/comments`
  with `path`, `line`, `commit_id`, `body`.
- One summary comment via
  `POST /repos/{owner}/{repo}/issues/{pr}/comments` (the issues API
  is GitHub's PR-comment-as-issue endpoint).

It will not:

- Edit or delete existing comments
- Push commits
- Approve or merge the PR
- Change labels

## Per-repo configuration in the dashboard

After the first review lands, visit the dashboard at
`https://reviewpilot.example.com/repos` to:

- Add new repos (the GitHub App will auto-pickup; the PAT token's
  scope must include the new repo)
- Change `max_input_tokens_per_pr` per repo (default 100000)
- Change `feedback_window_days` per repo (default 30)
- Disable / re-enable a repo

Settings persist in `webdashboard.SQLiteStore` and override the
global defaults at review time.

## Rate limits

GitHub's API limits:

- **REST** (used by `internal/platformgithub`): 5,000 req/h per PAT (or
  per App installation). ReviewPilot makes ~3–5 calls per review.
  Conservative estimate: 1,000 reviews/h before rate-limited.
- **Webhooks inbound**: unlimited (GitHub doesn't rate-limit incoming
  webhooks).

If you start hitting the rate limit:

1. Reduce `max_input_tokens_per_pr` so single-file PRs still fit but
   pathologically large diffs are skipped.
2. Enable the optional retry / circuit breaker (planned for v0.5.0).
3. Use multiple GitHub Apps with rotated PATs.

## Webhook security

ReviewPilot rejects any webhook whose
`X-Hub-Signature-256` header doesn't match `HMAC-SHA256(body, secret)`,
compared in constant time (`crypto/hmac.Equal`). Any mismatch returns
HTTP 401 and the request is **not** dispatched for review.

If you ever need to rotate the secret:

1. Update `RP_GITHUB_WEBHOOK_SECRET` on the server.
2. Update the secret on the GitHub App / webhook config.
3. Restart the server. Both must be in sync within the same secret
   rotation window or reviews fail.