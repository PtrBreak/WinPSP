# **WinPSP — Windows Pre‑Shutdown Processor**  
*A lightweight, file‑configured processor for the Windows PRESHUTDOWN phase.*

WinPSP leverages the Windows service model to execute a user‑defined command when the system enters the **PRESHUTDOWN** phase during shutdown.  
It blocks the shutdown process until the task completes or times out, providing a reliable and auditable mechanism for pre‑shutdown automation.

Typical use cases include:

- Flushing caches or buffers  
- Syncing data to remote storage  
- Gracefully stopping external systems  
- Running cleanup or archival scripts  
- Ensuring shutdown behavior is predictable and controlled  

WinPSP is written in Go, requires no dependencies, and runs as a single static executable.  
It also works on Windows Home editions (which lack Group Policy), making it a practical way to run shutdown scripts.  
WinPSP is a **pre‑shutdown executor**, not a “shutdown script system.” It does not rely on GPO or Task Scheduler.

Minimum supported Windows versions:  
- Windows Vista or Windows Server 2008 and later  
- Windows XP / Server 2003 and earlier **do not support PRESHUTDOWN**, so WinPSP cannot block shutdown on those systems  
  - Older systems only support the SHUTDOWN control code, which does **not** allow services to block shutdown  
  - Programs launched during shutdown may be terminated abruptly  

Windows 10/11 include a registry value that controls how long PRESHUTDOWN services may run before being forcibly terminated.  
The default is **5 seconds**, and must be increased manually if you need longer blocking behavior.

WinPSP can execute any executable program and supports passing arguments.  
This means:

> You can run another `.exe`, or invoke an interpreter (Python, PowerShell, etc.) to run a script.

However, note that Windows services behave differently from Group Policy shutdown scripts.  
Some commands are unreliable during PRESHUTDOWN—for example, PowerShell’s `Copy-Item` often fails because the .NET runtime becomes unstable during shutdown.

---

## Features

- Execute commands during the Windows **PRESHUTDOWN** phase  
- Block shutdown until tasks complete or timeout  
- Deterministic behavior via a simple JSON config file  
- No implicit logic or hidden behavior  
- Lightweight, no dependencies  
- Supports manual start, automatic start, or event‑triggered start  
- Supports Windows x86 / x64 / ARM64  
- Clear logging and error output  
- MIT licensed  

---

## Installation

### 1. Download the executable

Prebuilt binaries are provided:

- `winpsp-x64.exe` (Windows 64‑bit)  
- `winpsp-x86.exe` (Windows 32‑bit)  
- `winpsp-arm64.exe` (Windows ARM64)

Place the executable and config file in the fixed directory (not configurable in this version):

```
C:\ProgramData\WinPSP\
```

---

## Install the Service

Create the WinPSP service:

```
sc create WinPSP binPath= "C:\ProgramData\WinPSP\winpsp.exe" start= auto obj= LocalSystem
sc description WinPSP "Windows Pre-Shutdown Processor"
```

Start the service:

```
sc start WinPSP
```

---

## Configuration File

WinPSP uses a JSON configuration file.  
The config path is fixed, but the command you run can be anywhere.

Example:

```json
{
  "command": "cmd /C C:\\ProgramData\\winPSP\\shutdown.cmd",
  "log_count": 7,
  "timeout": 300
}
```

### Field Description

| Field | Type | Description |
|-------|------|-------------|
| **command** | string | Command to run when Windows enters the PRESHUTDOWN phase. Can be a batch file, script, or any executable. |
| **log_count** | integer | Number of log files to retain. |
| **timeout** | integer | Maximum number of seconds WinPSP will block shutdown. After timeout, WinPSP stops waiting and allows shutdown to continue. |

Config file location:

```
C:\ProgramData\WinPSP\config.json
```

WinPSP does **not** attempt to correct invalid negative values (e.g., `-1`).  
These are considered user errors and result in undefined behavior.  
Invalid values may cause out‑of‑range operations, skipped execution, or other unpredictable results.

If you insist on experimenting with negative values—well, enjoy the chaos. 🤭

### Default Values (when fields are missing)

- **command**: empty → no script is executed; shutdown is not blocked  
- **log_count**: `7`  
  - Different from `0` (which disables logging)  
  - Log filenames include timestamps, so lexicographical order equals chronological order  
- **timeout**: `300` seconds  
  - Different from `0` (which means wait indefinitely)  
  - Note: Windows also enforces its own global timeout via the registry

---

## Optional: Auto‑start WinPSP on Shutdown

If the service is set to **manual start**, you can still trigger it automatically using Task Scheduler.

Create the service without auto‑start:

```
sc create WinPSP binPath= "C:\ProgramData\WinPSP\winpsp.exe" obj= LocalSystem
```

Task Scheduler trigger:

- Log: `System`  
- Event ID: `1074`  
- Action: `sc start WinPSP`

This makes WinPSP run **only during shutdown**, consuming zero resources otherwise.

Task Scheduler XML (must be saved as UTF‑16LE):

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

Task Scheduler only **starts** WinPSP; it does not affect how WinPSP behaves during PRESHUTDOWN.

---

## Windows 10/11 Forced Service Termination Timeout

Registry path:

```
HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\WaitToKillServiceTimeout
```

Default value (Windows 15063+): **5000 ms** (5 seconds).  
Increase this value if your shutdown task needs more time.

Example:

```powershell
Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Control" `
    -Name "WaitToKillServiceTimeout" -Value "300000"
```

Notes:

- This value is **global**—WinPSP cannot be a special case  
- All services will be allowed to run longer  
- Windows only reads this value at boot; a reboot is required for changes to take effect

---

## Interactive Mode (Debugging)

You can test the configuration without installing the service:

```
winpsp --test-config
```

This validates the config file and writes logs to:

```
C:\ProgramData\WinPSP\
```

---

## Command Line Arguments

```
--test-config    Validate the config file and display parsed values
```

---

## How It Works

1. Windows begins shutdown and enters the **PRESHUTDOWN** phase  
2. WinPSP receives the `SERVICE_CONTROL_PRESHUTDOWN` control code  
3. WinPSP executes the configured command  
4. Shutdown is blocked until:  
   - The command completes, or  
   - The timeout is reached  
5. WinPSP exits, allowing shutdown to continue  

The entire process is deterministic and auditable.  
WinPSP does **not** attempt to delay or modify Windows’ shutdown logic—it only executes during PRESHUTDOWN and exits according to its rules.

---

## Building from Source

WinPSP is written in Go and does not require CGO.

```
go build .
```

---

## ⚠ Important: WinPSP **does NOT automatically invoke `cmd.exe`**

WinPSP executes the command **exactly as written**.  
It does **not** wrap commands in `cmd.exe /C` or PowerShell.

This means:

- WinPSP **does not** run `cmd.exe /C ...` automatically  
- WinPSP **does not** run PowerShell automatically  
- Batch syntax is **not** interpreted unless you explicitly call `cmd.exe`

### To run a batch file, you must write:

```
"command": "cmd /C C:\\Path\\script.cmd"
```

### Why this matters

Batch files (`.cmd` / `.bat`) often spawn **child cmd.exe processes**.  
If the parent cmd.exe exits early while child processes continue running, Windows considers the command “finished,” and WinPSP will:

- Stop blocking shutdown  
- Allow shutdown to continue  
- Even though the script is still running in the background  

Windows has no concept of a “process tree,” so WinPSP cannot detect child processes.

### Recommendations

If you must use batch files:

- Ensure the script does **not** spawn new cmd.exe instances  
- Or rewrite the logic in PowerShell  
- Or call it via `cmd /C` and ensure it runs synchronously  

WinPSP can only guarantee deterministic behavior when the command runs synchronously.

Commands that spawn child processes or cause early parent exit include:

- `timeout`
- `start`
- `exit 0` (without `/b`)
