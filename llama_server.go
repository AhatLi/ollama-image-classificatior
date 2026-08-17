package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LlamaServer manages a local llama-server child process: it launches it, waits
// until healthy, and can restart it when system memory usage crosses a threshold
// (llama-server memory grows over time and can OOM-kill the whole Termux app).
type LlamaServer struct {
	bin       string
	model     string
	mmproj    string
	host      string
	port      int
	ctxSize   int
	extraArgs []string
	healthURL string

	mu      sync.Mutex
	cmd     *exec.Cmd
	running bool
}

func NewLlamaServer(cfg *Config) *LlamaServer {
	return &LlamaServer{
		bin:       cfg.LlamaServerBin,
		model:     cfg.LlamaModel,
		mmproj:    cfg.LlamaMmproj,
		host:      cfg.LlamaHost,
		port:      cfg.LlamaPort,
		ctxSize:   cfg.LlamaCtxSize,
		extraArgs: cfg.LlamaExtraArgs,
		healthURL: fmt.Sprintf("http://%s:%d/health", cfg.LlamaHost, cfg.LlamaPort),
	}
}

// Start launches llama-server and blocks until it is healthy.
func (s *LlamaServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked()
}

func (s *LlamaServer) startLocked() error {
	args := []string{"-m", s.model}
	if s.mmproj != "" {
		args = append(args, "--mmproj", s.mmproj)
	}
	args = append(args, "--host", s.host, "--port", strconv.Itoa(s.port), "-c", strconv.Itoa(s.ctxSize))
	args = append(args, s.extraArgs...)

	cmd := exec.Command(s.bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if logf, err := os.OpenFile("llama-server.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("llama-server start failed: %w", err)
	}
	s.cmd = cmd
	s.running = true
	fmt.Printf("[llama-server] started (pid=%d), waiting for health...\n", cmd.Process.Pid)

	go func(cc *exec.Cmd) {
		_ = cc.Wait()
		s.mu.Lock()
		if s.cmd == cc {
			s.running = false
		}
		s.mu.Unlock()
	}(cmd)

	if err := s.waitHealthy(3 * time.Minute); err != nil {
		return err
	}
	fmt.Println("[llama-server] healthy")
	return nil
}

func (s *LlamaServer) waitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 4 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(s.healthURL)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.Contains(string(body), "ok") {
				return nil
			}
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("llama-server did not become healthy within %s", timeout)
}

func (s *LlamaServer) stopLocked() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	s.running = false
	s.cmd = nil
}

// Stop terminates llama-server.
func (s *LlamaServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// Restart stops and starts llama-server again.
func (s *LlamaServer) Restart() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Println("[llama-server] restarting...")
	s.stopLocked()
	time.Sleep(2 * time.Second)
	return s.startLocked()
}

func (s *LlamaServer) alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// MonitorMemory periodically checks system memory usage and restarts llama-server
// when usage crosses thresholdPct, or if the process has died.
func (s *LlamaServer) MonitorMemory(thresholdPct float64, interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if thresholdPct <= 0 {
		thresholdPct = 85
	}
	for {
		select {
		case <-stop:
			return
		case <-time.After(interval):
			if !s.alive() {
				fmt.Println("[monitor] llama-server not running -> restart")
				if err := s.Restart(); err != nil {
					fmt.Printf("[monitor] restart failed: %v\n", err)
				}
				continue
			}
			used, err := systemMemUsedPercent()
			if err != nil {
				continue
			}
			if used >= thresholdPct {
				fmt.Printf("[monitor] system memory %.1f%% >= %.1f%% -> restart llama-server\n", used, thresholdPct)
				if err := s.Restart(); err != nil {
					fmt.Printf("[monitor] restart failed: %v\n", err)
				}
			}
		}
	}
}

// systemMemUsedPercent returns (MemTotal-MemAvailable)/MemTotal*100 from /proc/meminfo.
func systemMemUsedPercent() (float64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var total, avail float64
	haveTotal, haveAvail := false, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			total = parseMeminfoKB(line)
			haveTotal = true
		} else if strings.HasPrefix(line, "MemAvailable:") {
			avail = parseMeminfoKB(line)
			haveAvail = true
		}
		if haveTotal && haveAvail {
			break
		}
	}
	if !haveTotal || total <= 0 {
		return 0, fmt.Errorf("could not read MemTotal")
	}
	if !haveAvail {
		return 0, fmt.Errorf("could not read MemAvailable")
	}
	return (total - avail) / total * 100, nil
}

func parseMeminfoKB(line string) float64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[1], 64)
	return v
}
