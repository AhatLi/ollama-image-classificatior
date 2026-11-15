package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// TaggerServer는 ONNX 태거 서비스(tagger/tagger_server.py)를 자식 프로세스로 띄우고 관리한다.
// llama-server 관리(LlamaServer)와 같은 방식: 시작 전 포트를 쥔 stale 프로세스 정리, 헬스체크, 죽으면 재시작.
type TaggerServer struct {
	python   string
	script   string
	modelDir string
	labels   string
	host     string
	port     int
	threads  int

	mu      sync.Mutex
	cmd     *exec.Cmd
	running bool
}

func NewTaggerServer(cfg *Config) *TaggerServer {
	return &TaggerServer{
		python:   cfg.TaggerPython,
		script:   cfg.TaggerScript,
		modelDir: cfg.TaggerModelDir,
		labels:   cfg.TaggerLabels,
		host:     "127.0.0.1",
		port:     cfg.TaggerPort,
		threads:  cfg.TaggerThreads,
	}
}

func (s *TaggerServer) baseURL() string {
	return fmt.Sprintf("http://%s:%d", s.host, s.port)
}

// Start는 태거 서비스를 띄우고 모델 로드가 끝나 헬스체크에 통과할 때까지 기다린다.
func (s *TaggerServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked()
}

func (s *TaggerServer) startLocked() error {
	s.ensurePortFree()

	args := []string{s.script,
		"--host", s.host, "--port", strconv.Itoa(s.port),
		"--threads", strconv.Itoa(s.threads),
		"--models", s.modelDir, "--labels", s.labels}
	cmd := exec.Command(s.python, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if logf, err := os.OpenFile("tagger-server.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("tagger server start failed: %w", err)
	}
	s.cmd = cmd
	s.running = true
	fmt.Printf("[tagger] started (pid=%d), loading models...\n", cmd.Process.Pid)

	done := make(chan struct{})
	go func(cc *exec.Cmd) {
		_ = cc.Wait()
		close(done)
		s.mu.Lock()
		if s.cmd == cc {
			s.running = false
		}
		s.mu.Unlock()
	}(cmd)

	if err := s.waitHealthy(done, 5*time.Minute); err != nil {
		if s.cmd == cmd {
			s.running = false
		}
		return err
	}
	fmt.Println("[tagger] healthy")
	return nil
}

func (s *TaggerServer) portInUse() bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// killStale은 우리 자식이 아닌, 같은 포트로 뜬 태거 서비스 프로세스를 SIGKILL 한다 (/proc 사용, 실패는 무시).
func (s *TaggerServer) killStale() int {
	self := os.Getpid()
	own := 0
	if s.cmd != nil && s.cmd.Process != nil {
		own = s.cmd.Process.Pid
	}
	scriptBase := filepath.Base(s.script)
	portArg := "--port " + strconv.Itoa(s.port) + " "
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	killed := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self || pid == own {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		cmdline := strings.ReplaceAll(string(raw), "\x00", " ") + " "
		if !strings.Contains(cmdline, scriptBase) || !strings.Contains(cmdline, portArg) {
			continue
		}
		fmt.Printf("[tagger] stale process pid=%d found -> kill\n", pid)
		_ = syscall.Kill(pid, syscall.SIGKILL)
		killed++
	}
	return killed
}

func (s *TaggerServer) ensurePortFree() {
	if !s.portInUse() {
		return
	}
	fmt.Printf("[tagger] port %d is still in use; cleaning up stale server(s)...\n", s.port)
	s.killStale()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if !s.portInUse() {
			return
		}
		time.Sleep(1 * time.Second)
	}
	fmt.Printf("[tagger] warning: port %d still in use after 30s; starting anyway\n", s.port)
}

func (s *TaggerServer) waitHealthy(done <-chan struct{}, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 4 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(s.baseURL() + "/health")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.Contains(string(body), "ok") {
				return nil
			}
		}
		select {
		case <-done: // 모델 파일 누락 등으로 바로 죽은 경우
			return fmt.Errorf("tagger server exited during startup (tagger-server.log 확인)")
		case <-time.After(1 * time.Second):
		}
	}
	return fmt.Errorf("tagger server did not become healthy within %s", timeout)
}

func (s *TaggerServer) stopLocked() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	s.running = false
	s.cmd = nil
}

// Stop은 태거 서비스를 종료한다.
func (s *TaggerServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// EnsureRunning은 서비스가 죽어 있으면 다시 띄운다. 살아 있으면 아무것도 하지 않는다.
func (s *TaggerServer) EnsureRunning() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	fmt.Println("[tagger] not running -> restart")
	s.stopLocked()
	return s.startLocked()
}

// Restart는 서비스를 강제로 다시 띄운다 (응답 없음 등).
func (s *TaggerServer) Restart() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Println("[tagger] restarting...")
	s.stopLocked()
	time.Sleep(1 * time.Second)
	return s.startLocked()
}

// Monitor는 주기적으로 프로세스 생존을 확인하고 죽었으면 재시작한다 (OOM 킬 등).
func (s *TaggerServer) Monitor(interval time.Duration, stop <-chan struct{}) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	for {
		select {
		case <-stop:
			return
		case <-time.After(interval):
			if err := s.EnsureRunning(); err != nil {
				fmt.Printf("[tagger] restart failed: %v\n", err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 클라이언트
// ---------------------------------------------------------------------------

// TagResult는 태거 서비스의 /classify 응답이다. 배열 필드는 그대로 메타에 옮겨 적기 위해 RawMessage로 둔다.
type TagResult struct {
	OK         bool            `json:"ok"`
	Error      string          `json:"error"`
	ContentTop json.RawMessage `json:"content_top"`
	Medium     string          `json:"medium"`
	MediumP    float64         `json:"medium_p"`
	NSFW       float64         `json:"nsfw"`
	Rating     string          `json:"rating"`
	Tags       json.RawMessage `json:"tags"`
	Chars      json.RawMessage `json:"chars"`
	W          int             `json:"w"`
	H          int             `json:"h"`
	Animated   bool            `json:"animated"`
	Emb        string          `json:"emb"`
	Labels     string          `json:"labels"`
	Models     string          `json:"models"`
}

// BadImageError는 이미지 자체의 문제(깨진 파일, 지원 안 하는 형식)로 태깅할 수 없을 때의 오류다.
// 이 오류만 에러 폴더 이동/err 줄 기록 대상이고, 그 외(서비스 다운, 타임아웃)는 재시도 대상이다.
type BadImageError struct{ msg string }

func (e *BadImageError) Error() string { return e.msg }

func isBadImage(err error) bool {
	var b *BadImageError
	return errors.As(err, &b)
}

type TaggerClient struct {
	url    string
	client *http.Client
	srv    *TaggerServer
}

func NewTaggerClient(srv *TaggerServer, timeout time.Duration) *TaggerClient {
	return &TaggerClient{url: srv.baseURL() + "/classify", client: &http.Client{Timeout: timeout}, srv: srv}
}

func (c *TaggerClient) classifyOnce(path string) (*TagResult, error) {
	body, _ := json.Marshal(map[string]string{"path": path})
	resp, err := c.client.Post(c.url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r TagResult
	if jerr := json.Unmarshal(data, &r); jerr != nil {
		return nil, fmt.Errorf("tagger 응답 파싱 실패 (HTTP %d): %w", resp.StatusCode, jerr)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return nil, &BadImageError{msg: r.Error}
	}
	if resp.StatusCode != http.StatusOK || !r.OK {
		return nil, fmt.Errorf("tagger HTTP %d: %s", resp.StatusCode, r.Error)
	}
	return &r, nil
}

// Classify는 이미지 하나를 태깅한다. 서비스 쪽 문제(연결 실패, 타임아웃, 5xx)면 서비스를 되살리고 한 번 더 시도한다.
func (c *TaggerClient) Classify(path string) (*TagResult, error) {
	r, err := c.classifyOnce(path)
	if err == nil || isBadImage(err) {
		return r, err
	}
	fmt.Printf("  tagger 요청 실패(%v) -> 서비스 확인 후 재시도\n", err)
	time.Sleep(2 * time.Second) // 프로세스가 죽은 경우 종료가 감지될 시간을 준다
	var rerr error
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		rerr = c.srv.Restart() // 응답 없이 멈춘 경우
	} else {
		rerr = c.srv.EnsureRunning()
	}
	if rerr != nil {
		return nil, fmt.Errorf("tagger 서비스 재시작 실패: %w", rerr)
	}
	return c.classifyOnce(path)
}
