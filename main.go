//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
)

const (
	serviceName        = "WinPSP"
	defaultConfigPath  = `C:\ProgramData\WinPSP\config.json`
	defaultLogCount    = 7
	defaultTimeoutSecs = 300 // 5 minutes
	logFilePrefix      = "winpsp-"
	logFileExt         = ".log"
)

type Config struct {
	Command  string `json:"command"`
	LogCount *int   `json:"log_count"`
	Timeout  *int   `json:"timeout"` // seconds
}

type winpspService struct {
	configPath string
	config     *Config
}

func main() {
	// 判断是否为服务模式
	isService, err := svc.IsWindowsService()
	if err != nil {
		// 出错时保守处理：按服务模式运行
		isService = true
	}
	isInteractive := !isService

	// 仅保留一个参数：--test-config（布尔开关）
	testMode := flag.Bool("test-config", false,
		"Validate config file without executing commands")
	flag.Parse()

	// -----------------------------
	// 服务模式
	// -----------------------------
	if !isInteractive {
		svc.Run(serviceName, &winpspService{configPath: defaultConfigPath})
		return
	}

	// -----------------------------
	// 交互模式：测试配置文件
	// -----------------------------
	if *testMode {
		fmt.Println("WinPSP version 0.1.2")
		fmt.Println("WinPSP: Testing config file...")

		data, err := os.ReadFile(defaultConfigPath)
		if err != nil {
			fmt.Printf("Config error: %v\n", err)
			return
		}

		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			fmt.Printf("JSON parse error: %v\n", err)
			return
		}

		// 字段检查
		if strings.TrimSpace(cfg.Command) == "" {
			fmt.Println("command: empty → do nothing")
		} else {
			fmt.Printf("command: %s\n", cfg.Command)
		}

		if cfg.Timeout == nil {
			fmt.Printf("timeout: default (%d seconds)\n", defaultTimeoutSecs)
		} else {
			fmt.Printf("timeout: %d seconds\n", *cfg.Timeout)
		}

		if cfg.LogCount == nil {
			fmt.Printf("log_count: default (%d files)\n", defaultLogCount)
		} else {
			fmt.Printf("log_count: %d files\n", *cfg.LogCount)
		}

		fmt.Println("Config test completed.")
		return
	}

	// -----------------------------
	// 交互模式：无参数 → 执行一次
	// -----------------------------
	fmt.Println("WinPSP version 0.1.2")
	fmt.Println("Running in interactive mode (debug).")

	s := &winpspService{configPath: defaultConfigPath}
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

// -------------------- 配置加载 --------------------

func (s *winpspService) loadConfig() error {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		// 配置不存在或无法读取 → 不执行任何命令，直接放行
		s.config = nil
		return err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// 配置损坏 → 不执行任何命令
		s.config = nil
		return err
	}

	cfg.Command = strings.TrimSpace(cfg.Command)
	if cfg.Command == "" {
		// 空命令也视为无配置
		s.config = nil
		return errors.New("empty command in config")
	}

	if cfg.LogCount == nil {
		v := defaultLogCount
		cfg.LogCount = &v
	}

	if cfg.Timeout == nil {
		v := defaultTimeoutSecs
		cfg.Timeout = &v
	}

	s.config = &cfg
	return nil
}

// 解析指令
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

	exitCode, timedOut, execErr := runCommandWithTimeout(
		s.config.Command,
		time.Duration(*s.config.Timeout)*time.Second,
	)

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
	if s.config == nil || s.config.LogCount == nil {
		// 不可能发生，因为 loadConfig 会填默认值
		// 但为了未来维护安全，可以保留默认行为
	} else if *s.config.LogCount == 0 {
		return nil, nil, nil
	}

	cfgDir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return nil, nil, err
	}

	// 日志轮换
	logCount := defaultLogCount
	if s.config != nil && s.config.LogCount != nil {
		logCount = *s.config.LogCount
	}

	if err := rotateLogs(cfgDir, logCount); err != nil {
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
