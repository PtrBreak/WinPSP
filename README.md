# **WinPSP — Windows Pre‑Shutdown Processor**  
*A lightweight, deterministic, and configurable processor for Windows’ PRESHUTDOWN phase.*

WinPSP is a Windows service designed to execute user‑defined commands during the **PRESHUTDOWN** phase of system shutdown.  
It blocks the shutdown sequence until all configured tasks have completed or timed out, providing a reliable and auditable mechanism for pre‑shutdown automation.

This tool is ideal for scenarios such as:

- Flushing caches or buffers  
- Syncing data to remote storage  
- Gracefully stopping external systems  
- Running cleanup or archival scripts  
- Ensuring deterministic shutdown behavior  

WinPSP is written in Go, requires no dependencies, and runs as a single static executable.
It also works on Windows Home Edition, which does not provide Group Policy support, allowing shutdown scripts to run even on systems without GPO.

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

- `winpsp-x64.exe` (Windows 64‑bit)  
- `winpsp-x86.exe` (Windows 32‑bit)  
- `winpsp-arm64.exe` (Windows ARM64)

Place the executable in a permanent directory, e.g.:

```
C:\ProgramData\WinPSP\
```

---

## Service Installation

Install the WinPSP service:

```
sc create WinPSP binPath= "C:\ProgramData\WinPSP\winpsp.exe" start= auto obj= LocalSystem
sc description WinPSP "Windows Pre-Shutdown Processor"
```

Start the service:

```
sc start WinPSP
```

---

## Configuration

WinPSP uses a JSON configuration file.  
Example:

```json
{
  "command": "cmd /C C:\\ProgramData\\winPSP\\shutdown.cmd",
  "log_count": 7,
  "timeout": 300
}
```

### Field description

| Field | Type | Description |
|-------|------|-------------|
| **command** | string | The command WinPSP will execute when the system enters the PRESHUTDOWN phase. This can be a batch file, PowerShell script, or any executable. |
| **log_count** | integer | Number of log lines to keep in WinPSP’s in‑memory ring buffer. |
| **timeout** | integer | Maximum number of seconds WinPSP will block shutdown while waiting for the command to finish. If the timeout is reached, WinPSP stops waiting and allows shutdown to continue. |

Specify the config path using:

```
WinPSP --config C:\ProgramData\WinPSP\config.json
```

When running as a service, this flag is passed via the service’s ImagePath.

---

## Optional: Start WinPSP Automatically on Shutdown

If the service is set to **manual start**, you can still have WinPSP automatically start during shutdown by using a Task Scheduler trigger.

Trigger configuration:

- Log: `System`
- Event ID: `1074`
- Action: `sc start WinPSP`

This allows WinPSP to run *only* during shutdown, consuming no resources during normal operation.

Task Scheduler XML example:

```xml
<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Author>WinPSP</Author>
    <Description>Start WinPSP service when shutdown/restart is initiated (Event ID 1074).</Description>
  </RegistrationInfo>
  <Triggers>
    <EventTrigger>
      <Enabled>true</Enabled>
      <Subscription>&lt;QueryList&gt;&lt;Query Id="0" Path="System"&gt;&lt;Select Path="System"&gt;*[System[(EventID=1074)]]&lt;/Select&gt;&lt;/Query&gt;&lt;/QueryList&gt;</Subscription>
    </EventTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>S-1-5-18</UserId>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>false</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>sc.exe</Command>
      <Arguments>start WinPSP</Arguments>
    </Exec>
  </Actions>
</Task>
```

---

## Interactive Mode (Debug)

You can test your configuration without installing the service:

```
winpsp --config C:\path\config.json
```

This runs a single PRESHUTDOWN cycle and prints logs to the console.

---

## Command‑Line Options

```
--config <path>     Run WinPSP with the specified config file
--help              Show help message
--version           Show version information
```

---

## How It Works

1. Windows begins shutdown and enters the **PRESHUTDOWN** phase  
2. WinPSP receives the `SERVICE_CONTROL_PRESHUTDOWN` control code  
3. WinPSP executes all configured commands sequentially  
4. Shutdown is blocked until:  
   - All commands finish, or  
   - A command hits its timeout  
5. WinPSP exits, allowing shutdown to continue  

This behavior is deterministic and fully auditable.

---

## Building from Source

WinPSP is written in Go and requires no CGO.
If you already use Go, simply run:

```
go build .
```

---

## ⚠ Important: WinPSP does NOT wrap commands with `cmd.exe`

WinPSP executes the configured command **exactly as provided**, without adding any shell wrapper.  
This means:

- WinPSP does **not** run `cmd.exe /C ...` automatically  
- WinPSP does **not** run PowerShell automatically  
- WinPSP does **not** interpret batch syntax unless you explicitly invoke `cmd.exe`

### If you want to run a batch file, you MUST write:

```
"command": "cmd /C C:\\Path\\script.cmd"
```

### Why this matters

Batch files (`.cmd` / `.bat`) often spawn **child shells**.  
If the outer shell exits early while inner shells continue running, Windows will think the command has finished, and WinPSP will:

- stop blocking shutdown  
- allow the system to continue shutting down  
- even though your script is still running in a child shell

This is a common pitfall when using batch files during shutdown.

### Recommendation

If you prefer to use batch files:

- Ensure the script does **not** spawn additional child `cmd.exe` processes  
- Or rewrite the logic in PowerShell  
- Or invoke the batch file through `cmd /C` and ensure it runs synchronously  

WinPSP can only guarantee deterministic behavior when the executed command runs synchronously.

Commands that may spawn child processes or cause the parent shell to exit prematurely include:

- `timeout`
- `start`
- `exit 0` (when used without the `/b` flag)
