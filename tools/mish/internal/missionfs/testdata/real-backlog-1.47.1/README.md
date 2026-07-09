This fixture was cut from Backlog.md 1.47.1 with:

```sh
backlog init perf-regression --defaults --integration-mode none --backlog-dir backlog --config-location folder --no-git --auto-open-browser false
```

U3 tests mutate copies of this real-cut config to exercise mish's pinned-key drift detection.

`task-files/task-1 - First-task.md` was cut from the same initialized board with:

```sh
backlog task create "First task" --status "To Do"
```
