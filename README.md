# Dashboard

A desktop dashboard built with [raylib](https://www.raylib.com/).

![Screenshot](./screenshot.png)

## Data sources

- Alertmanager alerts
- Github repositories
  - PRs
  - Issues
  - Workflow runs

## Configuration

Put something like this in `./config.json`:

```json
{
  "repos": ["raysan5/raylib"],
  "alerts": {
    "server": "alertmanager.example.com",
    "receiver": "myreceiver"
  }
}
```

## Usage

If you want to get data from private repositories, set `GH_TOKEN` to your github token. Otherwise, you don't need to set it.

```sh
GH_TOKEN=replace-me go run ./main.go
```
