//go:build windows

package main

import (
    "context"
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "golang.org/x/sys/windows/svc"
    "io"
    "io/fs"
    "os"
    "os/exec"
    "path/filepath"
    "sort"
    "strings"
    "time"
)

const (
    serviceName        = "WinPSP"
    defaultConfigPath  = `C:\ProgramData\WinPSP\config.json`
    defaultLogCount    = 7
    defaultTimeoutSecs = 300 // 5 minutes
    logFilePrefix      = "winpsp-"
    logFileExt         = ".log"
)

var (
    configPath  = flag.String("config", "", "Path to config file")
    showHelp    = flag.Bool("help", false, "Show help")
    showVersion = flag.Bool("version", false, "Show version")
)

type Config struct {
    Command  string `json:"command"`
    LogCount int    `json:"log_count"`
    Timeout  int    `json:"timeout"` // seconds
}

type winpspService struct {
    configPath string
    config     *Config
}

func main() {
    // 如果以服务方式运行，svc.IsAnInteractiveSession 会返回 false
    isInteractive, err := svc.IsAnInteractiveSession()
    if err != nil {
		// 出错时，保守起见当作服务模式
        isInteractive = false
    }

    flag.Parse()

    if *showHelp {
        printHelp()
        os.Exit(0)
    }

    if *showVersion {
        fmt.Println("WinPSP version 0.1")
        os.Exit(0)
    }

    if *configPath != "" {
        runWithConfig(*configPath)
        return
    }

    if !isInteractive {
		// 服务模式
        svc.Run(serviceName, &winpspService{configPath: *configPath})
        return
    }

	// 交互模式：简单跑一次 PRESHUTDOWN 流程，方便调试
    fmt.Println("Running in interactive mode (debug).")
    s := &winpspService{configPath: *configPath}
    if err := s.loadConfig(); err != nil {
        fmt.Printf("Config error: %v\n", err)
        fmt.Println("Nothing will be executed. Exiting.")
        return
    }
    if err := s.handleShutdownOnce(); err != nil {
        fmt.Printf("Shutdown handler error: %v\n", err)
    }
}

// -------------------- 服务实现 --------------------
func (s *winpspService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
    changes <- svc.Status{
        State:   svc.StartPending,
        Accepts: svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPreShutdown,
    }

    // 尝试加载配置（失败则标记为无配置模式）
    _ = s.loadConfig()

    changes <- svc.Status{
        State:   svc.Running,
        Accepts: svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPreShutdown,
    }

    for c := range r {
        switch c.Cmd {
        case svc.Interrogate:
            changes <- c.CurrentStatus
        case svc.Stop, svc.Shutdown:
            changes <- svc.Status{State: svc.StopPending}
            return false, 0
        case svc.PreShutdown:
            // 关机前执行
            changes <- svc.Status{State: svc.StopPending}
            _ = s.handleShutdownOnce()
            return false, 0
        default:
            // ignore
        }
    }

    return false, 0
}

func runWithConfig(path string) {
    s := &winpspService{configPath: path}

    if err := s.loadConfig(); err != nil {
        fmt.Printf("Config error: %v\n", err)
        fmt.Println("Nothing will be executed. Exiting.")
        return
    }

    if err := s.handleShutdownOnce(); err != nil {
        fmt.Printf("Shutdown handler error: %v\n", err)
    }
}

// -------------------- 配置加载 --------------------

func (s *winpspService) loadConfig() error {
    data, err := os.ReadFile(s.configPath)
    if err != nil {
        s.config = nil
        return err
    }

    var cfg Config
    if err := json.Unmarshal(data, &cfg); err != nil {
        s.config = nil
        return err
    }

    cfg.Command = strings.TrimSpace(cfg.Command)
    if cfg.Command == "" {
        s.config = nil
        return errors.New("empty command in config")
    }

    // 判断 JSON 是否包含 timeout 字段
    hasTimeoutField := strings.Contains(string(data), `"timeout"`)

    switch {
    case cfg.Timeout < 0:
        s.config = nil
        return errors.New("invalid timeout")

    case cfg.Timeout == 0 && hasTimeoutField:
        // 用户明确写了 0 → 禁用超时
        // 保留 0

    case cfg.Timeout == 0 && !hasTimeoutField:
        // 用户没写 timeout → 默认值
        cfg.Timeout = defaultTimeoutSecs

    case cfg.Timeout > 0:
        // 使用用户配置
    }

    if cfg.LogCount <= 0 {
        cfg.LogCount = defaultLogCount
    }

    s.config = &cfg
    return nil
}

// -------------------- 关机处理 --------------------

func (s *winpspService) handleShutdownOnce() error {
    // 无配置 → 什么也不做，直接放行
    if s.config == nil {
        return nil
    }

    logFile, logWriter, err := s.openLogFile()
    if err != nil {
        // 日志失败不影响执行，只是没有日志
        logFile = nil
        logWriter = nil
    } else {
        defer logFile.Close()
    }

    logLine := func(format string, args ...any) {
        if logWriter == nil {
            return
        }
        ts := time.Now().Format("2006-01-02 15:04:05")
        line := fmt.Sprintf(format, args...)
        fmt.Fprintf(logWriter, "[%s] %s\n", ts, line)
    }

    logLine("WinPSP: Shutdown triggered (PRESHUTDOWN)")
    logLine("Running: %s", s.config.Command)

    exitCode, timedOut, execErr := runCommandWithTimeout(s.config.Command, time.Duration(s.config.Timeout)*time.Second)

    if execErr != nil && !timedOut {
        logLine("Command error: %v", execErr)
    }
    if timedOut {
        logLine("Timeout after %d seconds", s.config.Timeout)
    } else {
        logLine("Exit code: %d", exitCode)
    }

    logLine("Shutdown released")
    return nil
}

// -------------------- 日志文件管理 --------------------

func (s *winpspService) openLogFile() (*os.File, io.Writer, error) {
    cfgDir := filepath.Dir(s.configPath)
    if err := os.MkdirAll(cfgDir, 0755); err != nil {
        return nil, nil, err
    }

    // 日志轮换
    if err := rotateLogs(cfgDir, s.config.LogCount); err != nil {
        // 轮换失败不阻止继续写新日志
    }

    ts := time.Now().Format("20060102-150405")
    logName := logFilePrefix + ts + logFileExt
    logPath := filepath.Join(cfgDir, logName)

    f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        return nil, nil, err
    }

    return f, f, nil
}

func rotateLogs(dir string, maxCount int) error {
    entries, err := os.ReadDir(dir)
    if err != nil {
        return err
    }

    var logs []fs.DirEntry
    for _, e := range entries {
        if e.IsDir() {
            continue
        }
        name := e.Name()
        if strings.HasPrefix(name, logFilePrefix) && strings.HasSuffix(name, logFileExt) {
            logs = append(logs, e)
        }
    }

    if len(logs) <= maxCount {
        return nil
    }

    sort.Slice(logs, func(i, j int) bool {
        return logs[i].Name() < logs[j].Name()
    })

    toDelete := logs[0 : len(logs)-maxCount]
    for _, e := range toDelete {
        _ = os.Remove(filepath.Join(dir, e.Name()))
    }

    return nil
}

func splitCommandLine(cmd string) ([]string, error) {
    var args []string
    var current strings.Builder
    inQuotes := false

    for i := 0; i < len(cmd); i++ {
        c := cmd[i]

        switch c {
        case '"':
            inQuotes = !inQuotes

        case ' ':
            if inQuotes {
                current.WriteByte(c)
            } else if current.Len() > 0 {
                args = append(args, current.String())
                current.Reset()
            }

        default:
            current.WriteByte(c)
        }
    }

    if inQuotes {
        return nil, errors.New("unmatched quotes in command line")
    }

    if current.Len() > 0 {
        args = append(args, current.String())
    }

    return args, nil
}

// -------------------- 命令执行（带超时） --------------------
func runCommandWithTimeout(commandLine string, timeout time.Duration) (exitCode int, timedOut bool, err error) {
    // 解析命令行
    parts, err := splitCommandLine(commandLine)
    if err != nil {
        return 1, false, err
    }
    if len(parts) == 0 {
        return 1, false, errors.New("empty command line")
    }

    exe := parts[0]
    args := parts[1:]

    // 禁用超时
    if timeout == 0 {
        cmd := exec.Command(exe, args...)
        err = cmd.Run()
        return exitCodeFromError(err), false, err
    }

    // 有超时
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()

    cmd := exec.CommandContext(ctx, exe, args...)
    err = cmd.Run()

    if ctx.Err() == context.DeadlineExceeded {
        return exitCodeFromError(err), true, err
    }

    return exitCodeFromError(err), false, err
}

func exitCodeFromError(err error) int {
    if err == nil {
        return 0
    }
    var exitErr *exec.ExitError
    if errors.As(err, &exitErr) {
        // Windows 下 ExitError.Sys() 里有退出码，但标准库没直接暴露
        // 简化处理：非 0 即视为 1
        return 1
    }
    return 1
}

// 帮助显示内容
func printHelp() {
    fmt.Println("WinPSP — Windows Pre-Shutdown Processor")
    fmt.Println()
    fmt.Println("Usage:")
    fmt.Println("  winpsp --config <path>     Run WinPSP with the specified config file")
    fmt.Println("  winpsp --help              Show this help message")
    fmt.Println("  winpsp --version           Show WinPSP version")
    fmt.Println()
    fmt.Println("Notes:")
    fmt.Println("  - When running as a Windows service, WinPSP ignores command-line flags")
    fmt.Println("    except --config, which specifies the path to the JSON configuration.")
    fmt.Println("  - WinPSP blocks system shutdown during the PRESHUTDOWN phase until all")
    fmt.Println("    configured commands have completed or timed out.")
}
