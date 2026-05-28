# Manual Gio Window Smoke

This runbook verifies the experimental read-only Gio window by hand. It is a
manual visual smoke, not a production UI acceptance test.

## Preconditions

- Go 1.25 or newer is available on `PATH`.
- Repository dependencies are already resolved. If in doubt, run the default
  local check first.
- A desktop session is available. Headless shells, SSH-only sessions, and CI
  runners usually cannot show the native window.
- `gopls` is not required. The experimental UI path must not start it
  automatically.

## Preflight

From the repository root on Windows PowerShell:

```powershell
go build -tags=gio ./cmd/kleiber
go run ./cmd/kleiber experimental-ui --smoke .
```

On other shells:

```bash
go build -tags=gio ./cmd/kleiber
go run ./cmd/kleiber experimental-ui --smoke .
```

The smoke command should print a concise model summary, include `window:
skipped (smoke mode)`, include the palette/navigation/refresh/quit shortcut
contract, and include `gopls: not auto-started`. It does not open a native
window and does not require `-tags=gio`.

## Launch

Windows PowerShell:

```powershell
go run -tags=gio ./cmd/kleiber experimental-ui .
```

Other shells:

```bash
go run -tags=gio ./cmd/kleiber experimental-ui .
```

The optional path argument defaults to the current directory.

## Expected Visual Result

- A native window opens with the title `Kleiber experimental UI`.
- The header/status area is visible.
- The status text says the UI is read-only/pre-alpha and that `gopls` is not
  auto-started.
- A project section is visible and shows the project root plus module/package
  counts when a Go project is opened.
- A buffers section is visible. It may say there are no open buffers.
- A commands section is visible.
- An editor section is visible and explicitly says the editor widget is pending.
- File tree interaction and command palette execution are shown as pending, not
  implemented.
- The window status line mentions window-level shortcuts: `F5` / `Ctrl+R` /
  `Command+R` to refresh the current UI state snapshot, and `Ctrl+Q` /
  `Command+Q` / `Escape` to quit.
- `Ctrl+P` / `Command+P` opens a command-palette shell. Up/Down changes the
  selected command, Escape closes the palette before quitting the window, and
  Enter is visible as execution-pending rather than executing a command.

This smoke does not validate typing, cursor movement, text editing, command
execution from the palette, file tree interaction, LSP behavior, or editor
widget behavior.

## Expected Close Behavior

- Close the native window using the OS window close control.
- Alternatively, press `Ctrl+Q`, `Command+Q`, or `Escape` while the window is
  focused.
- The terminal should return to the prompt. The `cmd/kleiber` Gio launcher owns
  this process lifecycle because Gio's `app.Main()` can block forever on
  desktop platforms. Window-loop errors and recovered window-loop panics should
  be reported in the terminal before the process exits.
- No `gopls` process should remain because the experimental UI does not start it.

`F5`, `Ctrl+R`, and `Command+R` only schedule an in-memory UI state snapshot
refresh. `Ctrl+P` / `Command+P` opens the palette shell, but Enter does not
execute commands yet. These shortcuts do not implement file tree interaction,
project auto-refresh, editor input, or LSP restart behavior.

On Windows PowerShell, check for a leftover `gopls` process:

```powershell
Get-Process gopls -ErrorAction SilentlyContinue
```

No output means no `gopls` process was found.

## Failure Capture

If the window fails to open, crashes, renders blank content, or does not exit:

1. Copy the full terminal output.
2. Note the OS, GPU/graphics environment, and whether this was a local desktop,
   remote desktop, SSH, WSL, VM, or CI session.
3. Note the exact command used and the project path argument.
4. If the process hangs, find and stop it carefully.

Windows PowerShell process inspection:

```powershell
Get-Process kleiber,go -ErrorAction SilentlyContinue | Select-Object Id,ProcessName,Path
```

Stop only the process you just launched:

```powershell
Stop-Process -Id <PID>
```

If unsure which process is safe to stop, leave it running and ask for help with
the captured process list.
