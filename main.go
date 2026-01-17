package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "embed"
)

//go:embed index.html
var indexHTML string

// Config 配置结构
type Config struct {
	UploadURL      string `json:"UPLOAD_URL"`
	ProjectURL     string `json:"PROJECT_URL"`
	AutoAccess     bool   `json:"AUTO_ACCESS"`
	FilePath       string `json:"FILE_PATH"`
	SubPath        string `json:"SUB_PATH"`
	Port           int    `json:"SERVER_PORT"`
	ExternalPort   int    `json:"EXTERNAL_PORT"`
	UUID           string `json:"UUID"`
	NezhaServer    string `json:"NEZHA_SERVER"`
	NezhaPort      string `json:"NEZHA_PORT"`
	NezhaKey       string `json:"NEZHA_KEY"`
	ArgoDomain     string `json:"ARGO_DOMAIN"`
	ArgoAuth       string `json:"ARGO_AUTH"`
	ArgoPort       int    `json:"ARGO_PORT"`
	CFIP           string `json:"CFIP"`
	CFPort         int    `json:"CFPORT"`
	Name           string `json:"NAME"`
	
	// 守护进程配置
	DaemonCheckInterval int `json:"DAEMON_CHECK_INTERVAL"`
	DaemonMaxRetries    int `json:"DAEMON_MAX_RETRIES"`
	DaemonRestartDelay  int `json:"DAEMON_RESTART_DELAY"`
}

// TunnelType 隧道类型
type TunnelType string

const (
	TunnelFixed     TunnelType = "fixed"
	TunnelToken     TunnelType = "token"
	TunnelTemporary TunnelType = "temporary"
)

// ProcessStatus 进程状态
type ProcessStatus struct {
	Running   bool      `json:"running"`
	Retries   int       `json:"retries"`
	LastStart time.Time `json:"lastStart"`
	Pid       int       `json:"pid"`
	Type      string    `json:"type,omitempty"`
	Domain    string    `json:"domain,omitempty"`
}

// DaemonStatus 守护进程状态
type DaemonStatus struct {
	mu        sync.RWMutex
	Processes map[string]*ProcessStatus `json:"processes"`
	Tunnel    struct {
		Type   TunnelType `json:"type"`
		Domain string     `json:"domain"`
	} `json:"tunnel"`
	Timestamp time.Time `json:"timestamp"`
	Uptime    float64   `json:"uptime"`
}

// DaemonManager 守护进程管理器
type DaemonManager struct {
	config       *Config
	status       *DaemonStatus
	processes    map[string]*exec.Cmd
	checkTickers map[string]*time.Ticker
	restartTimers map[string]*time.Timer
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// 全局变量
var (
	daemonManager *DaemonManager
	config        *Config
)

func init() {
	// 初始化随机种子
	rand.Seed(time.Now().UnixNano())
}

func randomInt(max int) int {
	b := make([]byte, 1)
	rand.Read(b)
	return int(b[0]) % max
}

// RandomName 生成随机名称
func RandomName() string {
	const chars = "abcdefghijklmnopqrstuvwxyz"
	result := make([]byte, 6)
	for i := range result {
		result[i] = chars[randomInt(len(chars))]
	}
	return string(result)
}

// NewConfig 从环境变量创建配置
func NewConfig() *Config {
	port, _ := strconv.Atoi(getEnv("SERVER_PORT", "3000"))
	externalPort, _ := strconv.Atoi(getEnv("EXTERNAL_PORT", "7860"))
	autoAccess, _ := strconv.ParseBool(getEnv("AUTO_ACCESS", "false"))
	
	cfg := &Config{
		UploadURL:      getEnv("UPLOAD_URL", ""),
		ProjectURL:     getEnv("PROJECT_URL", ""),
		AutoAccess:     autoAccess,
		FilePath:       getEnv("FILE_PATH", "./tmp"),
		SubPath:        getEnv("SUB_PATH", "sub"),
		Port:           port,
		ExternalPort:   externalPort,
		UUID:           getEnv("UUID", "4b3e2bfe-bde1-5def-d035-0cb572bbd046"),
		NezhaServer:    getEnv("NEZHA_SERVER", "gwwjllhldpjy.us-west-1.clawcloudrun.com:443"),
		NezhaPort:      getEnv("NEZHA_PORT", ""),
		NezhaKey:       getEnv("NEZHA_KEY", "rRA5ZrgOmsosl7EiyIuJBhnGwcAqWDUr"),
		ArgoDomain:     getEnv("ARGO_DOMAIN", "hug3.bgxzg.indevs.in"),
		ArgoAuth:       getEnv("ARGO_AUTH", `eyJhIjoiMzZhYzM1MmM5YmY2N2M1MzE0ZGJmYmE3MzFmMmIzMTkiLCJ0IjoiMWFhZmZiYmMtMTViZi00M2U0LTk1ZTUtZDdiMGJlODYxOTViIiwicyI6Ik9UUXdaV1EyTTJNdFpqUmhNUzAwWW1Sa0xUaG1ZVEl0WkdVeE5tTmpOR1F5WldaaiJ9`),
		ArgoPort:       getEnvInt("ARGO_PORT", 7860),
		CFIP:           getEnv("CFIP", "cdns.doon.eu.org"),
		CFPort:         getEnvInt("CFPORT", 443),
		Name:           getEnv("NAME", ""),
		
		DaemonCheckInterval: getEnvInt("DAEMON_CHECK_INTERVAL", 30000),
		DaemonMaxRetries:    getEnvInt("DAEMON_MAX_RETRIES", 5),
		DaemonRestartDelay:  getEnvInt("DAEMON_RESTART_DELAY", 10000),
	}
	
	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

// NewDaemonManager 创建守护进程管理器
func NewDaemonManager(cfg *Config) *DaemonManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	dm := &DaemonManager{
		config:        cfg,
		status:        &DaemonStatus{},
		processes:     make(map[string]*exec.Cmd),
		checkTickers:  make(map[string]*time.Ticker),
		restartTimers: make(map[string]*time.Timer),
		ctx:           ctx,
		cancel:        cancel,
	}
	
	dm.status.Processes = map[string]*ProcessStatus{
		"nezha":  {Running: false},
		"xray":   {Running: false},
		"tunnel": {Running: false},
	}
	
	// 加载保存的状态
	dm.loadStatus()
	
	return dm
}

func (dm *DaemonManager) loadStatus() {
	statusPath := filepath.Join(dm.config.FilePath, "daemon_status.json")
	if _, err := os.Stat(statusPath); err == nil {
		data, err := os.ReadFile(statusPath)
		if err == nil {
			json.Unmarshal(data, &dm.status)
		}
	}
}

func (dm *DaemonManager) saveStatus() {
	dm.status.mu.Lock()
	defer dm.status.mu.Unlock()
	
	dm.status.Timestamp = time.Now()
	dm.status.Uptime = time.Since(dm.status.Timestamp).Seconds()
	
	statusPath := filepath.Join(dm.config.FilePath, "daemon_status.json")
	data, _ := json.MarshalIndent(dm.status, "", "  ")
	os.WriteFile(statusPath, data, 0644)
}

func (dm *DaemonManager) setTunnelInfo(tunnelType TunnelType, domain string) {
	dm.status.mu.Lock()
	defer dm.status.mu.Unlock()
	
	dm.status.Tunnel.Type = tunnelType
	dm.status.Tunnel.Domain = domain
	
	log.Printf("Tunnel type set to: %s, domain: %s", tunnelType, domain)
	dm.saveStatus()
}

func (dm *DaemonManager) startProcess(name, command string, args []string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	log.Printf("Starting %s process...", name)
	
	cmd := exec.CommandContext(dm.ctx, command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	
	// 设置输出管道
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start %s: %v", name, err)
		dm.scheduleRestart(name, command, args)
		return err
	}
	
	dm.processes[name] = cmd
	dm.status.Processes[name] = &ProcessStatus{
		Running:   true,
		Retries:   0,
		LastStart: time.Now(),
		Pid:       cmd.Process.Pid,
	}
	
	// 处理输出
	go dm.handleProcessOutput(name, stdout, stderr)
	
	// 监控进程退出
	go dm.monitorProcess(name, cmd)
	
	// 启动健康检查
	dm.startHealthCheck(name)
	
	dm.saveStatus()
	
	return nil
}

func (dm *DaemonManager) handleProcessOutput(name string, stdout, stderr io.ReadCloser) {
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[%s] %s", name, line)
			
			if name == "tunnel" {
				dm.handleTunnelOutput(line)
			}
			
			if strings.Contains(line, "Connected") || 
			   strings.Contains(line, "ready") || 
			   strings.Contains(line, "started") || 
			   strings.Contains(line, "listening") {
				log.Printf("%s started successfully", name)
				dm.mu.Lock()
				if status, ok := dm.status.Processes[name]; ok {
					status.Retries = 0
				}
				dm.mu.Unlock()
			}
		}
	}()
	
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("[%s ERROR] %s", name, scanner.Text())
		}
	}()
}

func (dm *DaemonManager) handleTunnelOutput(output string) {
	// 检查临时隧道的域名
	if dm.status.Tunnel.Type == TunnelTemporary {
		if strings.Contains(output, "trycloudflare.com") {
			// 提取域名
			parts := strings.Split(output, "trycloudflare.com")
			if len(parts) > 0 {
				domain := strings.TrimPrefix(strings.TrimSuffix(parts[0], "https://"), "http://") + "trycloudflare.com"
				log.Printf("Temporary tunnel domain detected: %s", domain)
				
				if dm.status.Tunnel.Domain != domain {
					dm.setTunnelInfo(TunnelTemporary, domain)
					
					// 触发订阅更新
					go func() {
						time.Sleep(2 * time.Second)
						generateSubscription(dm.config, domain)
					}()
				}
			}
		}
	}
}

func (dm *DaemonManager) monitorProcess(name string, cmd *exec.Cmd) {
	err := cmd.Wait()
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	log.Printf("%s process exited with error: %v", name, err)
	delete(dm.processes, name)
	
	if status, ok := dm.status.Processes[name]; ok {
		status.Running = false
	}
	
	// 如果不是正常退出，尝试重启
	if err != nil {
		log.Printf("%s exited abnormally, scheduling restart...", name)
		dm.scheduleRestart(name, cmd.Path, cmd.Args[1:])
	}
	
	dm.saveStatus()
}

func (dm *DaemonManager) scheduleRestart(name, command string, args []string) {
	// 清除现有重启定时器
	if timer, ok := dm.restartTimers[name]; ok {
		timer.Stop()
		delete(dm.restartTimers, name)
	}
	
	dm.mu.Lock()
	status := dm.status.Processes[name]
	if status == nil {
		status = &ProcessStatus{}
		dm.status.Processes[name] = status
	}
	currentRetries := status.Retries
	status.Retries++
	dm.mu.Unlock()
	
	if currentRetries >= dm.config.DaemonMaxRetries {
		log.Printf("%s has reached maximum restart attempts (%d)", name, dm.config.DaemonMaxRetries)
		
		// 等待一段时间后再重试
		time.AfterFunc(60*time.Second, func() {
			dm.mu.Lock()
			if s, ok := dm.status.Processes[name]; ok {
				s.Retries = 0
			}
			dm.mu.Unlock()
			dm.scheduleRestart(name, command, args)
		})
		return
	}
	
	// 计算延迟时间（指数退避）
	delay := time.Duration(dm.config.DaemonRestartDelay) * time.Millisecond
	for i := 0; i < currentRetries; i++ {
		delay *= 2
	}
	if delay > 60*time.Second {
		delay = 60 * time.Second
	}
	
	log.Printf("Scheduling %s restart in %v (attempt %d/%d)", 
		name, delay, currentRetries+1, dm.config.DaemonMaxRetries)
	
	dm.restartTimers[name] = time.AfterFunc(delay, func() {
		log.Printf("Restarting %s...", name)
		dm.startProcess(name, command, args)
	})
}

func (dm *DaemonManager) startHealthCheck(name string) {
	// 清除现有检查定时器
	if ticker, ok := dm.checkTickers[name]; ok {
		ticker.Stop()
		delete(dm.checkTickers, name)
	}
	
	ticker := time.NewTicker(time.Duration(dm.config.DaemonCheckInterval) * time.Millisecond)
	dm.checkTickers[name] = ticker
	
	go func() {
		for range ticker.C {
			dm.checkProcessHealth(name)
		}
	}()
}

func (dm *DaemonManager) checkProcessHealth(name string) {
	dm.mu.RLock()
	cmd, ok := dm.processes[name]
	dm.mu.RUnlock()
	
	if !ok || cmd == nil || cmd.Process == nil {
		log.Printf("%s process not found, marking as dead", name)
		dm.mu.Lock()
		if status, ok := dm.status.Processes[name]; ok {
			status.Running = false
		}
		dm.mu.Unlock()
		return
	}
	
	// 检查进程是否存在
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		log.Printf("%s process (PID: %d) is dead", name, cmd.Process.Pid)
		dm.mu.Lock()
		if status, ok := dm.status.Processes[name]; ok {
			status.Running = false
		}
		delete(dm.processes, name)
		dm.mu.Unlock()
		dm.saveStatus()
	}
}

func (dm *DaemonManager) getAllStatus() map[string]interface{} {
	dm.status.mu.RLock()
	defer dm.status.mu.RUnlock()
	
	status := map[string]interface{}{
		"nezha": map[string]interface{}{
			"running":    dm.status.Processes["nezha"].Running,
			"retries":    dm.status.Processes["nezha"].Retries,
			"lastStart":  dm.status.Processes["nezha"].LastStart,
			"name":       "哪吒监控代理",
		},
		"xray": map[string]interface{}{
			"running":    dm.status.Processes["xray"].Running,
			"retries":    dm.status.Processes["xray"].Retries,
			"lastStart":  dm.status.Processes["xray"].LastStart,
			"name":       "Xray代理服务",
		},
		"tunnel": map[string]interface{}{
			"running":    dm.status.Processes["tunnel"].Running,
			"retries":    dm.status.Processes["tunnel"].Retries,
			"lastStart":  dm.status.Processes["tunnel"].LastStart,
			"name":       dm.getTunnelDisplayName(),
			"displayType": string(dm.status.Tunnel.Type),
			"domain":     dm.status.Tunnel.Domain,
		},
		"timestamp": time.Now(),
		"uptime":    dm.status.Uptime,
	}
	
	return status
}

func (dm *DaemonManager) getTunnelDisplayName() string {
	switch dm.status.Tunnel.Type {
	case TunnelFixed:
		return "Cloudflare固定隧道"
	case TunnelToken:
		return "Cloudflare Token隧道"
	case TunnelTemporary:
		return "Cloudflare临时隧道"
	default:
		return "Cloudflare隧道"
	}
}

func (dm *DaemonManager) cleanup() {
	log.Println("Cleaning up all daemon processes...")
	
	dm.cancel()
	
	// 清理定时器
	for name, ticker := range dm.checkTickers {
		ticker.Stop()
		delete(dm.checkTickers, name)
	}
	
	for name, timer := range dm.restartTimers {
		timer.Stop()
		delete(dm.restartTimers, name)
	}
	
	// 终止所有进程
	for name, cmd := range dm.processes {
		if cmd != nil && cmd.Process != nil {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		delete(dm.processes, name)
	}
	
	dm.saveStatus()
}

// HTTP处理函数
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	
	// 如果有index.html文件，则使用它
	if _, err := os.Stat("index.html"); err == nil {
		http.ServeFile(w, r, "index.html")
	} else {
		// 否则使用嵌入的HTML或默认消息
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if indexHTML != "" {
			w.Write([]byte(indexHTML))
		} else {
			fmt.Fprintf(w, "Hello world!")
		}
	}
}

func handleDaemonStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    daemonManager.getAllStatus(),
		"config": map[string]interface{}{
			"checkInterval": config.DaemonCheckInterval,
			"maxRetries":    config.DaemonMaxRetries,
			"restartDelay":  config.DaemonRestartDelay,
		},
	})
}

func handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	processName := strings.TrimPrefix(r.URL.Path, "/restart/")
	
	validProcesses := map[string]bool{
		"nezha":  true,
		"xray":   true,
		"tunnel": true,
		"all":    true,
	}
	
	if !validProcesses[processName] {
		http.Error(w, fmt.Sprintf("Invalid process name. Valid options: nezha, xray, tunnel, all"), http.StatusBadRequest)
		return
	}
	
	if processName == "all" {
		for name := range validProcesses {
			if name != "all" {
				// 触发重启
				daemonManager.scheduleRestart(name, "", nil)
			}
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "All processes restart initiated",
		})
	} else {
		daemonManager.scheduleRestart(processName, "", nil)
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("%s restart initiated", processName),
		})
	}
}

func handleSubscription(w http.ResponseWriter, r *http.Request) {
	subPath := filepath.Join(config.FilePath, "sub.txt")
	if data, err := os.ReadFile(subPath); err == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(data)
	} else {
		http.Error(w, "Subscription not available", http.StatusNotFound)
	}
}

// 生成Xray配置文件
func generateConfig() error {
	configData := map[string]interface{}{
		"log": map[string]interface{}{
			"access":   "/dev/null",
			"error":    "/dev/null",
			"loglevel": "none",
		},
		"dns": map[string]interface{}{
			"servers": []string{
				"https+local://8.8.8.8/dns-query",
				"https+local://1.1.1.1/dns-query",
				"8.8.8.8",
				"1.1.1.1",
			},
			"queryStrategy": "UseIP",
			"disableCache":  false,
		},
		"inbounds": []map[string]interface{}{
			{
				"port":     3001,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{{
						"id":   config.UUID,
						"flow": "xtls-rprx-vision",
					}},
					"decryption": "none",
					"fallbacks": []map[string]interface{}{
						{"dest": 3002},
						{"path": "/vless-argo", "dest": 3003},
						{"path": "/vmess-argo", "dest": 3004},
						{"path": "/trojan-argo", "dest": 3005},
					},
				},
				"streamSettings": map[string]interface{}{
					"network": "tcp",
				},
			},
			{
				"port":     3002,
				"listen":   "127.0.0.1",
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{{
						"id": config.UUID,
					}},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":   "tcp",
					"security":  "none",
				},
			},
			{
				"port":     3003,
				"listen":   "127.0.0.1",
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{{
						"id":    config.UUID,
						"level": 0,
					}},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":   "ws",
					"security":  "none",
					"wsSettings": map[string]interface{}{
						"path": "/vless-argo",
					},
				},
				"sniffing": map[string]interface{}{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
					"metadataOnly": false,
				},
			},
			{
				"port":     3004,
				"listen":   "127.0.0.1",
				"protocol": "vmess",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{{
						"id":      config.UUID,
						"alterId": 0,
					}},
				},
				"streamSettings": map[string]interface{}{
					"network": "ws",
					"wsSettings": map[string]interface{}{
						"path": "/vmess-argo",
					},
				},
				"sniffing": map[string]interface{}{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
					"metadataOnly": false,
				},
			},
			{
				"port":     3005,
				"listen":   "127.0.0.1",
				"protocol": "trojan",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{{
						"password": config.UUID,
					}},
				},
				"streamSettings": map[string]interface{}{
					"network":   "ws",
					"security":  "none",
					"wsSettings": map[string]interface{}{
						"path": "/trojan-argo",
					},
				},
				"sniffing": map[string]interface{}{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
					"metadataOnly": false,
				},
			},
		},
		"outbounds": []map[string]interface{}{
			{
				"protocol": "freedom",
				"tag":      "direct",
				"settings": map[string]interface{}{
					"domainStrategy": "UseIP",
				},
			},
			{
				"protocol": "blackhole",
				"tag":      "block",
			},
		},
		"routing": map[string]interface{}{
			"domainStrategy": "IPIfNonMatch",
			"rules":          []interface{}{},
		},
	}
	
	data, err := json.MarshalIndent(configData, "", "  ")
	if err != nil {
		return err
	}
	
	configPath := filepath.Join(config.FilePath, "config.json")
	return os.WriteFile(configPath, data, 0644)
}

// 下载文件
func downloadFile(fileName, fileUrl string) error {
	resp, err := http.Get(fileUrl)
	if err != nil {
		return fmt.Errorf("failed to download %s: %v", fileName, err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: status %d", fileName, resp.StatusCode)
	}
	
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read %s: %v", fileName, err)
	}
	
	filePath := filepath.Join(config.FilePath, fileName)
	if err := os.WriteFile(filePath, data, 0755); err != nil {
		return fmt.Errorf("failed to save %s: %v", fileName, err)
	}
	
	log.Printf("Downloaded %s successfully", fileName)
	return nil
}

// 获取系统架构
func getSystemArchitecture() string {
	arch := runtime.GOARCH
	if arch == "arm" || arch == "arm64" {
		return "arm"
	}
	return "amd"
}

// 下载所需文件
func downloadFiles() error {
	architecture := getSystemArchitecture()
	
	var files []struct {
		name string
		url  string
	}
	
	// 基础文件
	if architecture == "arm" {
		files = append(files, 
			struct{ name, url string }{"web", "https://arm64.ssss.nyc.mn/web"},
			struct{ name, url string }{"bot", "https://arm64.ssss.nyc.mn/bot"},
		)
	} else {
		files = append(files,
			struct{ name, url string }{"web", "https://amd64.ssss.nyc.mn/web"},
			struct{ name, url string }{"bot", "https://amd64.ssss.nyc.mn/bot"},
		)
	}
	
	// 哪吒代理文件
	if config.NezhaServer != "" && config.NezhaKey != "" {
		if config.NezhaPort != "" {
			if architecture == "arm" {
				files = append(files, struct{ name, url string }{"agent", "https://arm64.ssss.nyc.mn/agent"})
			} else {
				files = append(files, struct{ name, url string }{"agent", "https://amd64.ssss.nyc.mn/agent"})
			}
		} else {
			if architecture == "arm" {
				files = append(files, struct{ name, url string }{"v1", "https://arm64.ssss.nyc.mn/v1"})
			} else {
				files = append(files, struct{ name, url string }{"v1", "https://amd64.ssss.nyc.mn/v1"})
			}
		}
	}
	
	// 并行下载文件
	var wg sync.WaitGroup
	errChan := make(chan error, len(files))
	
	for _, file := range files {
		wg.Add(1)
		go func(name, url string) {
			defer wg.Done()
			if err := downloadFile(name, url); err != nil {
				errChan <- err
			}
		}(file.name, file.url)
	}
	
	wg.Wait()
	close(errChan)
	
	// 检查错误
	for err := range errChan {
		if err != nil {
			return err
		}
	}
	
	return nil
}

// 启动哪吒代理
func startNezhaAgent() error {
	if config.NezhaServer == "" || config.NezhaKey == "" {
		log.Println("NEZHA variables are empty, skipping Nezha agent")
		return nil
	}
	
	var cmd *exec.Cmd
	if config.NezhaPort == "" {
		// 使用php版本
		// 生成config.yaml
		configYaml := fmt.Sprintf(`
client_secret: %s
debug: false
disable_auto_update: true
disable_command_execute: false
disable_force_update: true
disable_nat: false
disable_send_query: false
gpu: false
insecure_tls: true
ip_report_period: 1800
report_delay: 4
server: %s
skip_connection_count: true
skip_procs_count: true
temperature: false
tls: %s
use_gitee_to_upgrade: false
use_ipv6_country_code: false
uuid: %s`,
			config.NezhaKey, config.NezhaServer, 
			strings.Contains(config.NezhaServer, ":443") || 
			strings.Contains(config.NezhaServer, ":8443") || 
			strings.Contains(config.NezhaServer, ":2096") || 
			strings.Contains(config.NezhaServer, ":2087") || 
			strings.Contains(config.NezhaServer, ":2083") || 
			strings.Contains(config.NezhaServer, ":2053"),
			config.UUID)
		
		configPath := filepath.Join(config.FilePath, "config.yaml")
		if err := os.WriteFile(configPath, []byte(configYaml), 0644); err != nil {
			return err
		}
		
		cmd = exec.Command(filepath.Join(config.FilePath, "v1"),
			"-c", configPath)
	} else {
		// 使用agent版本
		args := []string{
			"-s", fmt.Sprintf("%s:%s", config.NezhaServer, config.NezhaPort),
			"-p", config.NezhaKey,
			"--disable-auto-update",
			"--report-delay", "4",
			"--skip-conn",
			"--skip-procs",
		}
		
		// 检查是否需要TLS
		port, _ := strconv.Atoi(config.NezhaPort)
		tlsPorts := map[int]bool{443: true, 8443: true, 2096: true, 2087: true, 2083: true, 2053: true}
		if tlsPorts[port] {
			args = append(args, "--tls")
		}
		
		cmd = exec.Command(filepath.Join(config.FilePath, "agent"), args...)
	}
	
	return daemonManager.startProcess("nezha", cmd.Path, cmd.Args[1:])
}

// 启动Xray
func startXray() error {
	cmd := exec.Command(filepath.Join(config.FilePath, "web"),
		"-c", filepath.Join(config.FilePath, "config.json"))
	
	return daemonManager.startProcess("xray", cmd.Path, cmd.Args[1:])
}

// 分析隧道类型
func analyzeTunnelType() TunnelType {
	log.Println("Analyzing tunnel configuration...")
	
	if config.ArgoAuth != "" {
		if strings.Contains(config.ArgoAuth, "TunnelSecret") {
			log.Println("Tunnel type: FIXED (JSON configuration)")
			return TunnelFixed
		} else if len(config.ArgoAuth) >= 120 && len(config.ArgoAuth) <= 250 {
			log.Println("Tunnel type: TOKEN (Token authentication)")
			return TunnelToken
		}
	}
	
	log.Println("Tunnel type: TEMPORARY (Quick tunnel)")
	return TunnelTemporary
}

// 准备隧道配置
func prepareTunnelConfig(tunnelType TunnelType) error {
	switch tunnelType {
	case TunnelFixed:
		// 生成固定隧道配置文件
		var tunnelConfig map[string]interface{}
		if err := json.Unmarshal([]byte(config.ArgoAuth), &tunnelConfig); err != nil {
			return err
		}
		
		tunnelID, ok := tunnelConfig["TunnelID"].(string)
		if !ok {
			return fmt.Errorf("invalid tunnel configuration")
		}
		
		// 保存tunnel.json
		tunnelJSONPath := filepath.Join(config.FilePath, "tunnel.json")
		if err := os.WriteFile(tunnelJSONPath, []byte(config.ArgoAuth), 0644); err != nil {
			return err
		}
		
		// 生成tunnel.yml
		tunnelYAML := fmt.Sprintf(`tunnel: %s
credentials-file: %s
protocol: http2

ingress:
  - hostname: %s
    service: http://localhost:%d
    originRequest:
      noTLSVerify: true
  - service: http_status:404
`, tunnelID, tunnelJSONPath, config.ArgoDomain, config.ExternalPort)
		
		tunnelYAMLPath := filepath.Join(config.FilePath, "tunnel.yml")
		if err := os.WriteFile(tunnelYAMLPath, []byte(tunnelYAML), 0644); err != nil {
			return err
		}
		
		log.Println("Fixed tunnel configuration generated successfully")
		
	case TunnelToken:
		log.Println("Token tunnel requires no additional configuration")
		
	case TunnelTemporary:
		log.Println("Temporary tunnel requires no additional configuration")
	}
	
	return nil
}

// 启动Cloudflared隧道
func startCloudflaredTunnel(tunnelType TunnelType) error {
	botPath := filepath.Join(config.FilePath, "bot")
	if _, err := os.Stat(botPath); os.IsNotExist(err) {
		return fmt.Errorf("cloudflared binary not found")
	}
	
	var args []string
	switch tunnelType {
	case TunnelFixed:
		args = []string{
			"tunnel",
			"--edge-ip-version", "auto",
			"--config", filepath.Join(config.FilePath, "tunnel.yml"),
			"run",
		}
		log.Println("Starting fixed tunnel with YAML configuration")
		
	case TunnelToken:
		args = []string{
			"tunnel",
			"--edge-ip-version", "auto",
			"--no-autoupdate",
			"--protocol", "http2",
			"run",
			"--token", config.ArgoAuth,
		}
		
		if config.ArgoDomain != "" {
			args = append(args, "--hostname", config.ArgoDomain)
			log.Printf("Token tunnel with hostname: %s", config.ArgoDomain)
		} else {
			log.Println("Token tunnel without hostname (will use trycloudflare.com)")
			args = append(args,
				"--logfile", filepath.Join(config.FilePath, "boot.log"),
				"--loglevel", "info")
		}
		
		log.Println("Starting token tunnel")
		
	case TunnelTemporary:
		args = []string{
			"tunnel",
			"--edge-ip-version", "auto",
			"--no-autoupdate",
			"--protocol", "http2",
			"--logfile", filepath.Join(config.FilePath, "boot.log"),
			"--loglevel", "info",
			"--url", fmt.Sprintf("http://localhost:%d", config.ExternalPort),
		}
		log.Println("Starting temporary tunnel")
	}
	
	return daemonManager.startProcess("tunnel", botPath, args)
}

// 生成订阅
func generateSubscription(domain string) {
	if domain == "" {
		log.Println("No tunnel domain available for subscription generation")
		return
	}
	
	// 获取ISP信息
	isp := "Unknown"
	resp, err := http.Get("https://ipapi.co/json/")
	if err == nil {
		defer resp.Body.Close()
		var data map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			if country, ok := data["country_code"].(string); ok {
				if org, ok := data["org"].(string); ok {
					isp = fmt.Sprintf("%s_%s", country, org)
				}
			}
		}
	}
	
	nodeName := isp
	if config.Name != "" {
		nodeName = fmt.Sprintf("%s-%s", config.Name, isp)
	}
	
	// 生成VMESS配置
	vmess := map[string]interface{}{
		"v":    "2",
		"ps":   nodeName,
		"add":  config.CFIP,
		"port": config.CFPort,
		"id":   config.UUID,
		"aid":  "0",
		"scy":  "none",
		"net":  "ws",
		"type": "none",
		"host": domain,
		"path": "/vmess-argo?ed=2560",
		"tls":  "tls",
		"sni":  domain,
		"alpn": "",
		"fp":   "firefox",
	}
	
	vmessJSON, _ := json.Marshal(vmess)
	vmessBase64 := base64.StdEncoding.EncodeToString(vmessJSON)
	
	subTxt := fmt.Sprintf(`
vless://%s@%s:%d?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Fvless-argo%%3Fed%%3D2560#%s
  
vmess://%s
  
trojan://%s@%s:%d?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Ftrojan-argo%%3Fed%%3D2560#%s
`,
		config.UUID, config.CFIP, config.CFPort, domain, domain, nodeName,
		vmessBase64,
		config.UUID, config.CFIP, config.CFPort, domain, domain, nodeName,
	)
	
	encoded := base64.StdEncoding.EncodeToString([]byte(subTxt))
	
	subPath := filepath.Join(config.FilePath, "sub.txt")
	if err := os.WriteFile(subPath, []byte(encoded), 0644); err != nil {
		log.Printf("Failed to save subscription: %v", err)
	} else {
		log.Printf("Subscription saved to %s", subPath)
	}
	
	// 上传订阅
	go uploadSubscription(encoded)
}

// 上传订阅
func uploadSubscription(subscription string) {
	if config.UploadURL == "" {
		return
	}
	
	if config.ProjectURL != "" {
		subscriptionURL := fmt.Sprintf("%s/%s", config.ProjectURL, config.SubPath)
		data := map[string]interface{}{
			"subscription": []string{subscriptionURL},
		}
		
		jsonData, _ := json.Marshal(data)
		_, err := http.Post(config.UploadURL+"/api/add-subscriptions", 
			"application/json", 
			bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("Failed to upload subscription: %v", err)
		} else {
			log.Println("Subscription uploaded successfully")
		}
	}
}

// 添加访问任务
func addVisitTask() {
	if !config.AutoAccess || config.ProjectURL == "" {
		log.Println("Skipping adding automatic access task")
		return
	}
	
	data := map[string]string{
		"url": config.ProjectURL,
	}
	
	jsonData, _ := json.Marshal(data)
	_, err := http.Post("https://oooo.serv00.net/add-url", 
		"application/json", 
		bytes.NewBuffer(jsonData))
	
	if err != nil {
		log.Printf("Add automatic access task failed: %v", err)
	} else {
		log.Println("Automatic access task added successfully")
	}
}

// 监控隧道域名
func monitorTunnelDomain(tunnelType TunnelType) {
	log.Println("Starting tunnel domain monitoring...")
	
	// 等待隧道启动
	time.Sleep(10 * time.Second)
	
	switch tunnelType {
	case TunnelFixed, TunnelToken:
		if config.ArgoDomain != "" {
			log.Printf("Using fixed/token tunnel domain: %s", config.ArgoDomain)
			daemonManager.setTunnelInfo(tunnelType, config.ArgoDomain)
			generateSubscription(config.ArgoDomain)
		} else {
			extractDomainFromLogs(tunnelType)
		}
	case TunnelTemporary:
		extractDomainFromLogs(tunnelType)
	}
}

// 从日志中提取域名
func extractDomainFromLogs(tunnelType TunnelType) {
	bootLogPath := filepath.Join(config.FilePath, "boot.log")
	for i := 0; i < 10; i++ {
		if data, err := os.ReadFile(bootLogPath); err == nil {
			content := string(data)
			if strings.Contains(content, "trycloudflare.com") {
				lines := strings.Split(content, "\n")
				for _, line := range lines {
					if strings.Contains(line, "trycloudflare.com") {
						parts := strings.Split(line, "trycloudflare.com")
						if len(parts) > 0 {
							domain := strings.TrimPrefix(strings.TrimSuffix(parts[0], "https://"), "http://") + "trycloudflare.com"
							log.Printf("Extracted tunnel domain: %s", domain)
							daemonManager.setTunnelInfo(tunnelType, domain)
							generateSubscription(domain)
							return
						}
					}
				}
			}
		}
		time.Sleep(5 * time.Second)
	}
	log.Println("Failed to extract tunnel domain")
}

// 清理旧文件
func cleanupOldFiles() {
	files, err := os.ReadDir(config.FilePath)
	if err != nil {
		return
	}
	
	for _, file := range files {
		if file.Name() != "daemon_status.json" && !file.IsDir() {
			os.Remove(filepath.Join(config.FilePath, file.Name()))
		}
	}
}

// 删除节点
func deleteNodes() {
	if config.UploadURL == "" {
		return
	}
	
	subPath := filepath.Join(config.FilePath, "sub.txt")
	if _, err := os.Stat(subPath); os.IsNotExist(err) {
		return
	}
	
	data, err := os.ReadFile(subPath)
	if err != nil {
		return
	}
	
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return
	}
	
	lines := strings.Split(string(decoded), "\n")
	var nodes []string
	for _, line := range lines {
		if strings.Contains(line, "vless://") || 
		   strings.Contains(line, "vmess://") || 
		   strings.Contains(line, "trojan://") ||
		   strings.Contains(line, "hysteria2://") || 
		   strings.Contains(line, "tuic://") {
			nodes = append(nodes, line)
		}
	}
	
	if len(nodes) == 0 {
		return
	}
	
	jsonData, _ := json.Marshal(map[string]interface{}{"nodes": nodes})
	http.Post(config.UploadURL+"/api/delete-nodes", 
		"application/json", 
		bytes.NewBuffer(jsonData))
}

// 启动所有服务
func startAllServices() error {
	log.Println("Starting all services with daemon protection...")
	
	// 清理历史文件
	deleteNodes()
	cleanupOldFiles()
	
	// 生成Xray配置
	if err := generateConfig(); err != nil {
		return err
	}
	
	// 下载文件
	if err := downloadFiles(); err != nil {
		return err
	}
	
	// 分析隧道类型
	tunnelType := analyzeTunnelType()
	daemonManager.setTunnelInfo(tunnelType, config.ArgoDomain)
	
	// 准备隧道配置
	if err := prepareTunnelConfig(tunnelType); err != nil {
		return err
	}
	
	// 启动服务
	if err := startNezhaAgent(); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	
	if err := startXray(); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	
	if err := startCloudflaredTunnel(tunnelType); err != nil {
		return err
	}
	
	// 根据隧道类型设置等待时间
	if tunnelType == TunnelFixed {
		time.Sleep(5 * time.Second)
	} else {
		time.Sleep(10 * time.Second)
	}
	
	// 监控隧道域名
	go monitorTunnelDomain(tunnelType)
	
	// 添加保活任务
	go addVisitTask()
	
	log.Println("\n=== Server Initialization Complete ===")
	log.Printf("HTTP Service:      http://localhost:%d", config.Port)
	log.Printf("Proxy Service:     http://localhost:%d", config.ExternalPort)
	log.Printf("Daemon Status:     http://localhost:%d/daemon-status", config.Port)
	log.Printf("Subscription:      http://localhost:%d/%s", config.Port, config.SubPath)
	log.Printf("Tunnel Type:       %s", tunnelType)
	log.Printf("Tunnel Domain:     %s", config.ArgoDomain)
	log.Println("=====================================\n")
	
	return nil
}

// HTTP代理处理器
type ProxyHandler struct{}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	
	var target string
	if strings.HasPrefix(path, "/vless-argo") || 
	   strings.HasPrefix(path, "/vmess-argo") || 
	   strings.HasPrefix(path, "/trojan-argo") ||
	   path == "/vless" || 
	   path == "/vmess" || 
	   path == "/trojan" {
		target = "http://localhost:3001"
	} else {
		target = fmt.Sprintf("http://localhost:%d", config.Port)
	}
	
	url, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(url)
	proxy.ServeHTTP(w, r)
}

// 启动代理服务器
func startProxyServer() {
	proxy := &ProxyHandler{}
	
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.ExternalPort),
		Handler: proxy,
	}
	
	go func() {
		log.Printf("Proxy server is running on port:%d!", config.ExternalPort)
		log.Printf("HTTP traffic -> localhost:%d", config.Port)
		log.Printf("Xray traffic -> localhost:3001")
		
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Proxy server failed: %v", err)
		}
	}()
}

// 清理函数
func cleanup() {
	log.Println("\nReceived shutdown signal, cleaning up...")
	daemonManager.cleanup()
	os.Exit(0)
}

func main() {
	// 创建运行文件夹
	config = NewConfig()
	if _, err := os.Stat(config.FilePath); os.IsNotExist(err) {
		os.MkdirAll(config.FilePath, 0755)
		log.Printf("%s is created", config.FilePath)
	} else {
		log.Printf("%s already exists", config.FilePath)
	}
	
	// 初始化守护进程管理器
	daemonManager = NewDaemonManager(config)
	
	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cleanup()
	}()
	
	// 注册HTTP路由
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/daemon-status", handleDaemonStatus)
	http.HandleFunc("/restart/", handleRestart)
	http.HandleFunc("/"+config.SubPath, handleSubscription)
	
	// 启动HTTP服务器
	go func() {
		log.Printf("HTTP service is running on internal port:%d!", config.Port)
		log.Printf("Daemon endpoints:")
		log.Printf("  GET  /daemon-status  - Check all daemon processes status")
		log.Printf("  POST /restart/:name  - Restart specific process (nezha/xray/tunnel/all)")
		
		if err := http.ListenAndServe(fmt.Sprintf(":%d", config.Port), nil); err != nil {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()
	
	// 启动代理服务器
	startProxyServer()
	
	// 启动所有服务
	if err := startAllServices(); err != nil {
		log.Fatalf("Failed to start services: %v", err)
	}
	
	// 保持程序运行
	select {}
}
