# **WinBSP — Windows Pre‑Shutdown Processor**  
*A lightweight, deterministic, and configurable processor for Windows’ PRESHUTDOWN phase.*

WinBSP is a Windows service designed to execute user‑defined commands during the **PRESHUTDOWN** phase of system shutdown.  
It blocks the shutdown sequence until all configured tasks have completed or timed out, providing a reliable and auditable mechanism for pre‑shutdown automation.

This tool is ideal for scenarios such as:

- Flushing caches or buffers  
- Syncing data to remote storage  
- Gracefully stopping external systems  
- Running cleanup or archival scripts  
- Ensuring deterministic shutdown behavior  

WinBSP is written in Go, requires no dependencies, and runs as a single static executable.
It also functions on Windows Home Edition, providing shutdown‑script capability on systems that lack Group Policy support.

---

## Features

- Executes commands during Windows’ **PRESHUTDOWN** phase  
- Blocks shutdown until tasks complete or timeout  
- Deterministic, config‑driven behavior  
- No fallback logic, no ambiguity  
- Lightweight and dependency‑free  
- Supports manual start, automatic start, and event‑triggered start  
- Works on Windows 32‑bit, 64‑bit, and ARM64  
- Clean logging and error reporting  
- MIT licensed  

---

## Installation

### 1. Download the binary

Prebuilt binaries are available for:

- `winbsp-x64.exe` (Windows 64‑bit)  
- `winbsp-x86.exe` (Windows 32‑bit)  
- `winbsp-arm64.exe` (Windows ARM64)

Place the executable in a permanent directory, e.g.:

```
C:\ProgramData\WinBSP\
```

---

## Service Installation

Install the WinBSP service:

```
sc create WinBSP binPath= "C:\ProgramData\WinBSP\winbsp.exe" start= demand obj= LocalSystem
sc description WinBSP "Windows Pre-Shutdown Processor"
```

Start the service:

```
sc start WinBSP
```

---

## Configuration

WinBSP uses a JSON configuration file.  
Example:

```json
{
  "command": "cmd /C C:\\ProgramData\\winbsp\\shutdown.cmd",
  "log_count": 7,
  "timeout": 300
}
```

### Field description

| Field | Type | Description |
|-------|------|-------------|
| **command** | string | The command WinBSP will execute when the system enters the PRESHUTDOWN phase. This can be a batch file, PowerShell script, or any executable. |
| **log_count** | integer | Number of log lines to keep in WinBSP’s in‑memory ring buffer. |
| **timeout** | integer | Maximum number of seconds WinBSP will block shutdown while waiting for the command to finish. If the timeout is reached, WinBSP stops waiting and allows shutdown to continue. |

Specify the config path using:

```
winbsp --config C:\ProgramData\WinBSP\config.json
```

When running as a service, this flag is passed via the service’s ImagePath.

---

## Optional: Start WinBSP Automatically on Shutdown

If the service is set to **manual**, you can still ensure it runs during shutdown by creating a Task Scheduler trigger for **Event ID 1074**:

- Log: `System`  
- Event ID: `1074`  
- Action: `sc start WinBSP`

This allows WinBSP to run *only* during shutdown, consuming zero resources otherwise.

---

## Interactive Mode (Debug)

You can test your configuration without installing the service:

```
winbsp --config C:\path\config.json
```

This runs a single PRESHUTDOWN cycle and prints logs to the console.

---

## Command‑Line Options

```
--config <path>     Run WinBSP with the specified config file
--help              Show help message
--version           Show version information
```

---

## How It Works

1. Windows begins shutdown and enters the **PRESHUTDOWN** phase  
2. WinBSP receives the `SERVICE_CONTROL_PRESHUTDOWN` control code  
3. WinBSP executes all configured commands sequentially  
4. Shutdown is blocked until:  
   - All commands finish, or  
   - A command hits its timeout  
5. WinBSP exits, allowing shutdown to continue  

This behavior is deterministic and fully auditable.

---

## Building from Source

WinBSP is written in Go and requires no CGO.
If you already use Go, simply run:

```
go build .
```

---

## ⚠ Important: WinBSP does NOT wrap commands with `cmd.exe`

WinBSP executes the configured command **exactly as provided**, without adding any shell wrapper.  
This means:

- WinBSP does **not** run `cmd.exe /C ...` automatically  
- WinBSP does **not** run PowerShell automatically  
- WinBSP does **not** interpret batch syntax unless you explicitly invoke `cmd.exe`

### If you want to run a batch file, you MUST write:

```
"command": "cmd /C C:\\Path\\script.cmd"
```

### Why this matters

Batch files (`.cmd` / `.bat`) often spawn **child shells**.  
If the outer shell exits early while inner shells continue running, Windows will think the command has finished, and WinBSP will:

- stop blocking shutdown  
- allow the system to continue shutting down  
- even though your script is still running in a child shell

This is a common pitfall when using batch files during shutdown.

### Recommendation

If you use batch files:

- Ensure your script does **not** spawn additional shells  
- Or rewrite the logic in PowerShell  
- Or call the batch file through `cmd /C` and ensure it runs synchronously

WinBSP guarantees deterministic behavior only when the configured command behaves synchronously.
