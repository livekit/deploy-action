# LiveKit Cloud Agents GitHub Plugin

A GitHub Action for creating, deploying, and getting the status of LiveKit Cloud Agents.

> **Versions:** pin the action to a release tag. The examples below use `v2.12.2`, the latest release. The multi-environment inputs (`AGENT_ID`, `CONFIG`, `DEPLOYMENT`, `ATTRIBUTES`) and the `promote` operation need `v2.19.0` or later. Older versions ignore inputs they don't know, with only an "Unexpected input" warning. For example, a `deploy` with `DEPLOYMENT: staging` on an older version deploys to production.

## Usage

### Manually Create a New Agent or Deploy a new version

The create or deploy operations in this workflow are triggered manually and requires a working directory input.
**Automatic Deployment**: When creating a new agent, a deployment happens automatically. You do **not** need to manually call the deploy operation after creation.

```yaml
name: Create or Deploy LiveKit Cloud Agent Manually
on:
  workflow_dispatch:
    inputs:
      working_directory:
        description: 'Working directory for the agent'
        required: true
        type: string
        default: '.'
      operation:
        description: 'Which operation to run'
        required: true
        type: choice
        options:
          - create
          - deploy
        default: 'create'

jobs:
  create-agent:
    runs-on: ubuntu-latest
    environment: ${{ github.event.inputs.working_directory }}
    if: github.event.inputs.operation == 'create'
    concurrency:
      group: ${{ github.workflow }}-${{ github.ref }}
      cancel-in-progress: true
    permissions:
      contents: write
      pull-requests: write
      actions: read
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Create LiveKit Cloud Agent
        id: livekit
        uses: livekit/deploy-action@v2.12.2
        env:
          LIVEKIT_URL: ${{ secrets.LIVEKIT_URL }}
          LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
          LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
          SECRET_LIST: ${{ secrets.SECRET_LIST }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        with:
          OPERATION: create
          WORKING_DIRECTORY: ${{ github.event.inputs.working_directory }}
      - name: Create Pull Request # create a pull request to add the newly created livekit.toml
        uses: peter-evans/create-pull-request@v7
        with:
          add-paths: |
            ${{ github.event.inputs.working_directory }}/livekit.toml
          token: ${{ secrets.GITHUB_TOKEN }}
          branch: cloud-agent-${{ github.run_id }}
          title: "Add LiveKit agent config"
          commit-message: "Add LiveKit agent config"
          body: |
            This PR adds the LiveKit agent configuration

          base: main
          delete-branch: true
      - name: Checkout PR branch
        uses: actions/checkout@v4
        with:
          ref: cloud-agent-${{ github.run_id }}
      - name: Status Check # block until the agent is in the 'Running' state
        uses: livekit/deploy-action@v2.12.2
        env:
          LIVEKIT_URL: ${{ secrets.LIVEKIT_URL }}
          LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
          LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
        with:
          OPERATION: status-retry
          WORKING_DIRECTORY: ${{ github.event.inputs.working_directory }}
          TIMEOUT: 5m
  deploy-agent:
    runs-on: ubuntu-latest
    environment: ${{ github.event.inputs.working_directory }}
    if: github.event.inputs.operation == 'deploy'
    concurrency:
      group: ${{ github.workflow }}-${{ github.ref }}
      cancel-in-progress: true

    steps:
      - uses: actions/checkout@v4

      - name: Deploy LiveKit Cloud Agent
        uses: livekit/deploy-action@v2.12.2
        env:
          LIVEKIT_URL: ${{ secrets.LIVEKIT_URL }}
          LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
          LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
          SECRET_LIST: ${{ secrets.SECRET_LIST }}
        with:
          OPERATION: deploy
          WORKING_DIRECTORY: ${{ github.event.inputs.working_directory }}
```

### Deploy an Existing Agent on a file change

```yaml
name: Deploy test-agent on changes
on:
  push:
    branches:
      - main
    paths:
      - 'test-agent/**'
      - '!test-agent/livekit.toml'
      - '!test-agent/README.md'
      - '!test-agent/**/*.md'

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: test-agent
    concurrency:
      group: ${{ github.workflow }}-test-agent
      cancel-in-progress: true

    steps:
      - uses: actions/checkout@v4

      - name: Deploy LiveKit Cloud Agent
        uses: livekit/deploy-action@v2.12.2
        env:
          LIVEKIT_URL: ${{ secrets.LIVEKIT_URL }}
          LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
          LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
          SECRET_LIST: ${{ secrets.SECRET_LIST }}
        with:
          OPERATION: deploy
          WORKING_DIRECTORY: test-agent
```

### Check Agent Status

```yaml
name: Monitor Agent Status
on:
  schedule:
    - cron: '*/30 * * * *'  # Every 30 minutes

jobs:
  check-status:
    runs-on: ubuntu-latest
    concurrency:
      group: ${{ github.workflow }}-${{ github.ref }}
      cancel-in-progress: true
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Check Agent Status
        uses: livekit/deploy-action@v2.12.2
        env:
          LIVEKIT_URL: ${{ secrets.LIVEKIT_URL }}
          LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
          LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
        with:
          OPERATION: status
          SLACK_TOKEN: ${{ secrets.SLACK_BOT_TOKEN }}
          SLACK_CHANNEL: "#monitoring"
```

### Check Agent Status with Retry until timeout or status == Running
```yaml
      - name: Status Check
        uses: livekit/deploy-action@v2.12.2
        env:
          LIVEKIT_URL: ${{ secrets.LIVEKIT_URL }}
          LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
          LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
        with:
          OPERATION: status-retry
          WORKING_DIRECTORY: ${{ github.event.inputs.working_directory }}
          TIMEOUT: 5m
```

## Deploying to multiple environments

The action supports three ways to deploy one agent codebase to several environments:

1. [A project per environment](#a-project-per-environment): a separate LiveKit project for each environment, with one agent in each.
2. [An agent per environment](#an-agent-per-environment-in-one-project): one project, with a different agent for each environment.
3. [A deployment per environment](#a-deployment-per-environment-on-one-agent): one agent, with a [non-production deployment](https://docs.livekit.io/deploy/agents/deployments/) such as `staging` that you promote to production.

`LIVEKIT_URL`, `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` must be set in the job or step `env`, not in `with:`. Read the key and secret from `secrets`. The URL isn't sensitive, so it can be a `vars` variable.

After a `deploy`, run `status-retry` if later steps depend on a healthy rollout. It waits until the targeted deployment reports a healthy status. It doesn't check which version is serving, so right after a deploy it can pass while the previous version is still running.

### A project per environment

Create a [GitHub environment](https://docs.github.com/en/actions/managing-workflow-runs-and-deployments/managing-deployments/managing-environments-for-deployment) for each LiveKit project, such as `staging` and `production`. In each environment, set:

- The secrets `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` (plus `SECRET_LIST` if you use it).
- The variables `LIVEKIT_URL` and `AGENT_ID`. `AGENT_ID` is the ID of that project's agent.

`AGENT_ID` overrides `livekit.toml`, so one checkout deploys to whichever project the job's environment points at. Add required reviewers to the `production` environment to gate the second job.

```yaml
name: Deploy agent
on:
  push:
    branches: [main]

jobs:
  deploy-staging:
    runs-on: ubuntu-latest
    environment: staging
    concurrency:
      group: deploy-agent-staging
      cancel-in-progress: false
    env:
      LIVEKIT_URL: ${{ vars.LIVEKIT_URL }}
      LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
      LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
    steps:
      - uses: actions/checkout@v4

      - name: Check AGENT_ID is set
        if: vars.AGENT_ID == ''
        run: |
          echo "::error::Set the AGENT_ID variable in this environment"
          exit 1

      - name: Deploy
        uses: livekit/deploy-action@v2.19.0
        env:
          SECRET_LIST: ${{ secrets.SECRET_LIST }}
        with:
          OPERATION: deploy
          AGENT_ID: ${{ vars.AGENT_ID }}
          ATTRIBUTES: '{"git_commit": "${{ github.sha }}", "environment": "staging"}'

      - name: Wait for the rollout
        uses: livekit/deploy-action@v2.19.0
        with:
          OPERATION: status-retry
          AGENT_ID: ${{ vars.AGENT_ID }}
          TIMEOUT: 10m

  deploy-production:
    needs: deploy-staging
    runs-on: ubuntu-latest
    environment: production
    concurrency:
      group: deploy-agent-production
      cancel-in-progress: false
    env:
      LIVEKIT_URL: ${{ vars.LIVEKIT_URL }}
      LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
      LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
    steps:
      - uses: actions/checkout@v4

      - name: Check AGENT_ID is set
        if: vars.AGENT_ID == ''
        run: |
          echo "::error::Set the AGENT_ID variable in this environment"
          exit 1

      - name: Deploy
        uses: livekit/deploy-action@v2.19.0
        env:
          SECRET_LIST: ${{ secrets.SECRET_LIST }}
        with:
          OPERATION: deploy
          AGENT_ID: ${{ vars.AGENT_ID }}
          ATTRIBUTES: '{"git_commit": "${{ github.sha }}", "environment": "production"}'

      - name: Wait for the rollout
        uses: livekit/deploy-action@v2.19.0
        with:
          OPERATION: status-retry
          AGENT_ID: ${{ vars.AGENT_ID }}
          TIMEOUT: 10m
```

`deploy-production` waits for `deploy-staging`, so production is never deployed if staging fails. The guard step matters: if `AGENT_ID` is empty, the action falls back to `livekit.toml`, which may point at another environment's agent.

### An agent per environment in one project

All environments share one set of credentials and differ only in the agent ID. There are two ways to choose the agent.

**`AGENT_ID` from an environment variable.** Use the [project-per-environment workflow](#a-project-per-environment), with `LIVEKIT_URL`, `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` set once at the repository level instead of in each environment. Keep the guard step. Every agent is in the same project, so the subdomain check can't catch a fallback to the wrong `livekit.toml`.

**A toml per environment with `CONFIG`.** Commit one toml per environment next to the agent, for example `livekit.staging.toml` and `livekit.production.toml`. Each holds the same `[project] subdomain` and a different `[agent] id`. `CONFIG` is relative to `WORKING_DIRECTORY`. `WORKING_DIRECTORY` stays the build context.

```yaml
name: Deploy agent
on:
  push:
    branches: [main]

jobs:
  deploy-staging:
    runs-on: ubuntu-latest
    environment: staging
    concurrency:
      group: deploy-agent-staging
      cancel-in-progress: false
    env:
      LIVEKIT_URL: ${{ vars.LIVEKIT_URL }}
      LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
      LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
    steps:
      - uses: actions/checkout@v4

      - name: Deploy
        uses: livekit/deploy-action@v2.19.0
        env:
          SECRET_LIST: ${{ secrets.SECRET_LIST }}
        with:
          OPERATION: deploy
          WORKING_DIRECTORY: agent
          CONFIG: livekit.staging.toml

      - name: Wait for the rollout
        uses: livekit/deploy-action@v2.19.0
        with:
          OPERATION: status-retry
          WORKING_DIRECTORY: agent
          CONFIG: livekit.staging.toml

  deploy-production:
    needs: deploy-staging
    runs-on: ubuntu-latest
    environment: production
    concurrency:
      group: deploy-agent-production
      cancel-in-progress: false
    env:
      LIVEKIT_URL: ${{ vars.LIVEKIT_URL }}
      LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
      LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
    steps:
      - uses: actions/checkout@v4

      - name: Deploy
        uses: livekit/deploy-action@v2.19.0
        env:
          SECRET_LIST: ${{ secrets.SECRET_LIST }}
        with:
          OPERATION: deploy
          WORKING_DIRECTORY: agent
          CONFIG: livekit.production.toml

      - name: Wait for the rollout
        uses: livekit/deploy-action@v2.19.0
        with:
          OPERATION: status-retry
          WORKING_DIRECTORY: agent
          CONFIG: livekit.production.toml
```

To create the agent for a new environment, run `create` once with `CONFIG` set to the new file name. The action writes the agent ID to that file.

### A deployment per environment on one agent

A [non-production deployment](https://docs.livekit.io/deploy/agents/deployments/) runs a separate copy of an agent alongside production. Deploy to it with `DEPLOYMENT`, check it, then `promote` its image to production without a rebuild. Non-production deployments need the Ship plan or higher.

In this workflow, the `production` GitHub environment has required reviewers, so `promote` waits for approval. Both jobs talk to the same LiveKit project, so use repository secrets or the same values in both environments.

`promote` promotes whatever staging is running when it executes, not a specific commit. The workflow-level `concurrency` group keeps a newer push from replacing staging while a promotion waits for approval. Newer runs queue until the pending one is approved or rejected, and only the newest queued run is kept.

```yaml
name: Stage and promote agent
on:
  push:
    branches: [main]

concurrency:
  group: deploy-agent
  cancel-in-progress: false

jobs:
  stage:
    runs-on: ubuntu-latest
    environment: staging
    env:
      LIVEKIT_URL: ${{ vars.LIVEKIT_URL }}
      LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
      LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
    steps:
      - uses: actions/checkout@v4

      - name: Deploy to staging
        uses: livekit/deploy-action@v2.19.0
        with:
          OPERATION: deploy
          DEPLOYMENT: staging
          ATTRIBUTES: '{"git_commit": "${{ github.sha }}"}'

      - name: Wait for staging
        uses: livekit/deploy-action@v2.19.0
        with:
          OPERATION: status-retry
          DEPLOYMENT: staging
          TIMEOUT: 10m

  promote:
    needs: stage
    runs-on: ubuntu-latest
    environment: production
    env:
      LIVEKIT_URL: ${{ vars.LIVEKIT_URL }}
      LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
      LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
    steps:
      - uses: actions/checkout@v4

      - name: Promote staging to production
        uses: livekit/deploy-action@v2.19.0
        with:
          OPERATION: promote
          DEPLOYMENT: staging

      - name: Wait for production
        uses: livekit/deploy-action@v2.19.0
        with:
          OPERATION: status-retry
          TIMEOUT: 10m
```

Things to know about deployments:

- **Secrets are shared.** Every deployment of an agent reads the same secrets. Passing `SECRET_LIST` on a `DEPLOYMENT: staging` deploy changes the secrets production reads too, so the action logs a warning. To vary values per deployment, read `LIVEKIT_AGENT_DEPLOYMENT` in your agent and use prefixed secret names, as described in the [deployments docs](https://docs.livekit.io/deploy/agents/deployments/#branch-on-deployment-name-at-runtime).
- **Sleeping is healthy.** An idle non-production deployment sleeps, so `status` and `status-retry` with `DEPLOYMENT` pass on `Running` or `Sleeping`. Production must be `Running` in every region.
- **`delete` with `DEPLOYMENT` deletes only that deployment.** Without `DEPLOYMENT`, `delete` deletes the whole agent, including production.
- **Names** must start and end with a letter or digit, use only letters, digits, `-`, `_` and `.`, and be at most 63 characters. `production` is reserved: leave `DEPLOYMENT` empty to target production.

### How the action picks the agent

For `deploy`, `promote`, `status`, `status-retry` and `delete`, the action logs which source it used:

1. `AGENT_ID`, if set. No toml is read.
2. Otherwise the toml at `CONFIG`, relative to `WORKING_DIRECTORY`.
3. Otherwise `livekit.toml` in `WORKING_DIRECTORY`.

When the ID comes from a toml, the toml's `[project] subdomain` must match the subdomain of `LIVEKIT_URL`, or the action fails before it changes anything. Files named `livekit*.toml`, and the toml in use, are left out of the uploaded build context.

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `OPERATION` | Operation to perform: `create`, `deploy`, `promote`, `status`, `status-retry`, `delete` or `delete-multi` | Yes | `status` |
| `REGION` | Region to create the agent in. Used by `create` only. If empty, defaults to the nearest LiveKit Cloud region. | No | `""` |
| `WORKING_DIRECTORY` | Directory containing the agent. It's also the build context. | No | `.` |
| `AGENT_ID` | Agent to target. Takes precedence over `CONFIG` and `livekit.toml`; no toml is needed. Not allowed with `create`. | No | `""` |
| `CONFIG` | Path to the agent toml, relative to `WORKING_DIRECTORY`, e.g. `livekit.stage.toml` or `stage/livekit.toml`. With `create`, it's the file to write. | No | `livekit.toml` |
| `DEPLOYMENT` | Non-production deployment to target, e.g. `staging`. Empty targets production. Required for `promote`. Not allowed with `create` or `delete-multi`. | No | `""` |
| `ATTRIBUTES` | JSON object of string key/values to record on the new version, e.g. `{"git_commit": "abc123"}`. Used by `create` and `deploy`. | No | `""` |
| `AGENT_IDS` | Comma-separated agent IDs to delete. Used by `delete-multi` only. | No | `""` |
| `SLACK_TOKEN` | Slack Bot Token for sending notifications | No | - |
| `SLACK_CHANNEL` | Slack channel to send notifications to (e.g., `#general`) | No | - |
| `TIMEOUT` | Timeout for the status-retry check | No | 5m |

### Operations

| Operation | What it does |
|-----------|--------------|
| `create` | Creates a new agent from `WORKING_DIRECTORY` and writes its ID to `livekit.toml` (or `CONFIG`). Does nothing if that file exists. |
| `deploy` | Builds and deploys a new version to production, or to `DEPLOYMENT` when set. |
| `promote` | Promotes the image running in `DEPLOYMENT` to production without a rebuild. |
| `status` | Fails unless the targeted deployment is healthy. |
| `status-retry` | Polls `status` every 5 seconds until it passes or `TIMEOUT` is reached. |
| `delete` | Deletes the agent, or only `DEPLOYMENT` when set. |
| `delete-multi` | Deletes every agent in `AGENT_IDS`. |

## Environment Variables

### Required LiveKit Configuration

Set these in the job or step `env` (not `with:`):

- `LIVEKIT_URL` - Your LiveKit Cloud URL
- `LIVEKIT_API_KEY` - Your LiveKit Cloud API Key  
- `LIVEKIT_API_SECRET` - Your LiveKit Cloud API Secret

### Agent Secrets

Pass any number of secrets to your agent by setting the `SECRET_LIST` var with a comma separated list in your workflow:

```yaml
  OPENAI_API_KEY=${{key}},AUTH_TOKEN=${{token}}
  # Add as many secrets as needed...
```

Secrets are shared by every deployment of an agent. See [A deployment per environment on one agent](#a-deployment-per-environment-on-one-agent).

> **`SECRET_` prefixed variables aren't supported.** The action runs in a container and forwards a fixed set of variables into it. Individual `SECRET_<NAME>` variables aren't forwarded, so they never reach your agent. Use `SECRET_LIST` instead.


## Concurrency Control

All workflows should use concurrency control to prevent multiple operations on the same agent:

```yaml
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
```

## Permissions

The create operation performs git commits and pushes, so workflows need proper permissions:

```yaml
permissions:
  contents: write
  pull-requests: write
  actions: read
```

And the checkout action should include the token:

```yaml
- uses: actions/checkout@v4
  with:
    token: ${{ secrets.GITHUB_TOKEN }}
```

## Release

The release version lives in `VERSION`. The Makefile and action image tag both read from that file.

This project shares the same **major** and **minor** version as server-sdk-go for compatibility. The deploy-action must always import server-sdk-go with matching major and minor versions.

| valid | deploy-action | server-sdk-go | server constraint | explanation
✅	| 2.12.2 | 2.12.2 | >= 2.12.0 | minor/major version match
✅	| 2.12.5 | 2.12.2 | >= 2.12.0 | patch version ahead
❌	| 2.13.2 | 2.12.2 | >= 2.12.0 | minor version ahead
❌	| 3.12.2 | 2.12.2 | >= 2.12.0 | major version ahead
