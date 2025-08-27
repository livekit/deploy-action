# LiveKit Cloud Agents GitHub Plugin

A GitHub Action for creating, deploying, and getting the status of LiveKit Cloud Agents.

## Usage

### Create a New Agent

The create operation can only be triggered manually and requires a working directory input:

```yaml
name: Create LiveKit Cloud Agent
on:
  workflow_dispatch:
    inputs:
      working_directory:
        description: 'Working directory for the agent'
        required: true
        default: '/'
        type: string

jobs:
  create-agent:
    runs-on: ubuntu-latest
    concurrency:
      group: ${{ github.workflow }}-${{ github.ref }}
      cancel-in-progress: true
    permissions:
      contents: write
      pull-requests: write
    
    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.GITHUB_TOKEN }}
      
      - name: Create LiveKit Cloud Agent
        uses: livekit/cloud-agents-github-plugin@main
        env:
          SECRET_OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          SECRET_AUTH_TOKEN: ${{ secrets.AUTH_TOKEN }}
          LIVEKIT_URL: ${{ secrets.LIVEKIT_URL }}
          LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
          LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
        with:
          OPERATION: create
          WORKING_DIRECTORY: ${{ github.event.inputs.working_directory }}
```

### Deploy an Existing Agent

```yaml
name: Deploy LiveKit Cloud Agent
on:
  push:
    branches: [main]
    paths: ['my-agent/**']

jobs:
  deploy-agent:
    runs-on: ubuntu-latest
    concurrency:
      group: ${{ github.workflow }}-${{ github.ref }}
      cancel-in-progress: true
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Deploy LiveKit Cloud Agent
        uses: livekit/cloud-agents-github-plugin@main
        env:
          SECRET_OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          LIVEKIT_URL: ${{ secrets.LIVEKIT_URL }}
          LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
          LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
        with:
          OPERATION: deploy
          WORKING_DIRECTORY: ${{ github.event.inputs.working_directory }}
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
        uses: livekit/cloud-agents-github-plugin@main
        env:
          LIVEKIT_URL: ${{ secrets.LIVEKIT_URL }}
          LIVEKIT_API_KEY: ${{ secrets.LIVEKIT_API_KEY }}
          LIVEKIT_API_SECRET: ${{ secrets.LIVEKIT_API_SECRET }}
        with:
          OPERATION: status
          SLACK_TOKEN: ${{ secrets.SLACK_BOT_TOKEN }}
          SLACK_CHANNEL: "#monitoring"
```

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `OPERATION` | Operation to perform (`create`, `deploy`, `status`) | Yes | `status` |
| `WORKING_DIRECTORY` | Directory containing the agent configuration | No | `.` |
| `SLACK_TOKEN` | Slack Bot Token for sending notifications | No | - |
| `SLACK_CHANNEL` | Slack channel to send notifications to (e.g., `#general`) | No | - |

## Environment Variables

### Required LiveKit Configuration

These can be set either as direct environment variables or as secrets with `SECRET_` prefix:

- `LIVEKIT_URL` - Your LiveKit Cloud URL
- `LIVEKIT_API_KEY` - Your LiveKit Cloud API Key  
- `LIVEKIT_API_SECRET` - Your LiveKit Cloud API Secret

### Agent Secrets

Pass any number of secrets to your agent by prefixing them with `SECRET_`:

```yaml
env:
  SECRET_OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
  SECRET_AUTH_TOKEN: ${{ secrets.AUTH_TOKEN }}
  # Add as many secrets as needed...
```


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
```

And the checkout action should include the token:

```yaml
- uses: actions/checkout@v4
  with:
    token: ${{ secrets.GITHUB_TOKEN }}
```
