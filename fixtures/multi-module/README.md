# multi-module fixture

A deliberately-incomplete monorepo: three independent Go modules
under a shared root with **no `go.work`** file. Used by
`internal/doctor/workspace_test.go` and ad-hoc runs of
`kleiber doctor ./fixtures/multi-module` to verify the workspace
check fires and proposes a `go work init` command.

Per `fixtures/README.md`:

> Files in `/fixtures/` are deliberately broken or unusual Go
> programs used by tests. Do not "fix" them.

In particular, do **not** add a `go.work` here — its absence is the
property under test.

## Layout

```
fixtures/multi-module/
├── README.md      (this file)
├── a/             module example.com/multi/a
│   ├── go.mod
│   └── main.go
├── b/             module example.com/multi/b
│   ├── go.mod
│   └── main.go
└── c/             module example.com/multi/c
    ├── go.mod
    └── main.go
```

The top-level `fixtures/multi-module/` has no `go.mod` of its own —
again on purpose, to mirror the "monorepo without a workspace" case
that Project Doctor flags.
