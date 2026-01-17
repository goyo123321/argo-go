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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// ==============================
// 配置结构体
// ==============================
type Config struct {
	UploadURL      string `json:"upload_url"`
	ProjectURL     string `json:"project_url"`
	AutoAccess     bool   `json:"auto_access"`
	FilePath       string `json:"file_path"`
	SubPath        string `json:"sub_path"`
	Port           int    `json:"port"`
	ExternalPort   int    `json:"external_port"`
	UUID           string `json:"uuid"`
	NezhaServer    string `json:"nezha_server"`
	NezhaPort      string `json:"nezha_port"`
	NezhaKey       string `json:"nezha_key"`
	ArgoDomain     string `json:"argo_domain"`
	ArgoAuth       string `json:"argo_auth"`
	ArgoPort       int    `json:"argo_port"`
	CfIP           string `json:"cf_ip"`
	CfPort         int    `json:"cf_port"`
	Name           string `json:"name"`
	
	// 守护进程配置
	DaemonCheckInterval int `json:"daemon_check_interval"`
	DaemonMaxRetries    int `json:"daemon_max_retries"`
	DaemonRestartDelay  int `json:"daemon_restart_delay"`
}

// ==============================
// 进程状态
// ==============================
type ProcessStatus struct {
	Running    bool      `json:"running"`
	Retries    int       `json:"retries"`
	LastStart  time.Time `json:"last_start"`
	LastExit   time.Time `json:"last_exit,omitempty"`
	PID        int       `json:"pid,omitempty"`
	Type       string    `json:"type,omitempty"`
	Domain     string    `json:"domain,omitempty"`
	Name       string    `json:"name,omitempty"`
}

// ==============================
// 隧道类型常量
// ==============================
const (
	TunnelTypeFixed    = "fixed"
	TunnelTypeToken    = "token"
	TunnelTypeTemporary = "temporary"
)

// ==============================
// 守护进程管理器
// ==============================
type DaemonManager struct {
	config     *Config
	processes  map[string]*exec.Cmd
	status     map[string]*ProcessStatus
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	
	// 隧道信息
	tunnelType   string
	tunnelDomain string
	checkTimers  map[string]*time.Timer
	restartTimers map[string]*time.Timer
}

// ==============================
// 服务器实例
// ==============================
type Server struct {
	config   *Config
	daemon   *DaemonManager
	router   *mux.Router
	logger   *logrus.Logger
	httpServer *http.Server
	proxyServer *http.Server
}

// ==============================
// 初始化函数
// ==============================
func NewServer() (*Server, error) {
	// 加载配置
	cfg := loadConfig()
	
	// 创建目录
	if err := os.MkdirAll(cfg.FilePath, 0755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %v", err)
	}
	
	// 初始化日志
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})
	logger.SetLevel(logrus.InfoLevel)
	
	// 创建守护管理器
	ctx, cancel := context.WithCancel(context.Background())
	dm := &DaemonManager{
		config:       cfg,
		processes:    make(map[string]*exec.Cmd),
		status:       make(map[string]*ProcessStatus),
		ctx:          ctx,
		cancel:       cancel,
		checkTimers:  make(map[string]*time.Timer),
		restartTimers: make(map[string]*time.Timer),
	}
	
	// 创建服务器
	s := &Server{
		config: cfg,
		daemon: dm,
		router: mux.NewRouter(),
		logger: logger,
	}
	
	// 设置路由器
	s.setupRoutes()
	
	return s, nil
}

// ==============================
// 环境变量处理
// ==============================
func loadConfig() *Config {
	// 默认值
	defaultConfig := &Config{
		FilePath:       "./tmp",
		SubPath:        "sub",
		Port:           3000,
		ExternalPort:   7860,
		UUID:           "35461c1b-c9fb-efd5-e5d4-cf754d37bd4b",
		CfIP:           "cdns.doon.eu.org",
		CfPort:         443,
		ArgoPort:       7860,
		DaemonCheckInterval: 30000,
		DaemonMaxRetries:    5,
		DaemonRestartDelay:  10000,
	}
	
	// 从环境变量覆盖
	if val := os.Getenv("FILE_PATH"); val != "" {
		defaultConfig.FilePath = val
	}
	if val := os.Getenv("SUB_PATH"); val != "" {
		defaultConfig.SubPath = val
	}
	if val := os.Getenv("PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			defaultConfig.Port = port
		}
	}
	if val := os.Getenv("EXTERNAL_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			defaultConfig.ExternalPort = port
		}
	}
	if val := os.Getenv("UUID"); val != "" {
		defaultConfig.UUID = val
	}
	if val := os.Getenv("UPLOAD_URL"); val != "" {
		defaultConfig.UploadURL = val
	}
	if val := os.Getenv("PROJECT_URL"); val != "" {
		defaultConfig.ProjectURL = val
	}
	if val := os.Getenv("AUTO_ACCESS"); val != "" {
		if auto, err := strconv.ParseBool(val); err == nil {
			defaultConfig.AutoAccess = auto
		}
	}
	if val := os.Getenv("NEZHA_SERVER"); val != "" {
		defaultConfig.NezhaServer = val
	}
	if val := os.Getenv("NEZHA_PORT"); val != "" {
		defaultConfig.NezhaPort = val
	}
	if val := os.Getenv("NEZHA_KEY"); val != "" {
		defaultConfig.NezhaKey = val
	}
	if val := os.Getenv("ARGO_DOMAIN"); val != "" {
		defaultConfig.ArgoDomain = val
	}
	if val := os.Getenv("ARGO_AUTH"); val != "" {
		defaultConfig.ArgoAuth = val
	}
	if val := os.Getenv("ARGO_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			defaultConfig.ArgoPort = port
		}
	}
	if val := os.Getenv("CFIP"); val != "" {
		defaultConfig.CfIP = val
	}
	if val := os.Getenv("CFPORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			defaultConfig.CfPort = port
		}
	}
	if val := os.Getenv("NAME"); val != "" {
		defaultConfig.Name = val
	}
	if val := os.Getenv("DAEMON_CHECK_INTERVAL"); val != "" {
		if interval, err := strconv.Atoi(val); err == nil {
			defaultConfig.DaemonCheckInterval = interval
		}
	}
	if val := os.Getenv("DAEMON_MAX_RETRIES"); val != "" {
		if retries, err := strconv.Atoi(val); err == nil {
			defaultConfig.DaemonMaxRetries = retries
		}
	}
	if val := os.Getenv("DAEMON_RESTART_DELAY"); val != "" {
		if delay, err := strconv.Atoi(val); err == nil {
			defaultConfig.DaemonRestartDelay = delay
		}
	}
	
	return defaultConfig
}

// ==============================
// 生成随机名称
// ==============================
func generateRandomName(length int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, length)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// ==============================
// 守护进程管理器方法
// ==============================
func NewDaemonManager(cfg *Config) *DaemonManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &DaemonManager{
		config:       cfg,
		processes:    make(map[string]*exec.Cmd),
		status:       make(map[string]*ProcessStatus),
		ctx:          ctx,
		cancel:       cancel,
		checkTimers:  make(map[string]*time.Timer),
		restartTimers: make(map[string]*time.Timer),
	}
}

func (dm *DaemonManager) StartProcess(name, command string, args []string, options ...func(*exec.Cmd)) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	// 如果进程已存在，先停止
	if cmd, exists := dm.processes[name]; exists && cmd.Process != nil {
		cmd.Process.Kill()
	}
	
	// 创建命令
	cmd := exec.Command(command, args...)
	
	// 应用选项
	for _, option := range options {
		option(cmd)
	}
	
	// 设置默认选项
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	// 启动进程
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动进程 %s 失败: %v", name, err)
	}
	
	// 保存进程
	dm.processes[name] = cmd
	
	// 更新状态
	dm.status[name] = &ProcessStatus{
		Running:   true,
		Retries:   0,
		LastStart: time.Now(),
		PID:       cmd.Process.Pid,
		Name:      name,
	}
	
	// 设置隧道类型（如果是隧道进程）
	if name == "tunnel" {
		dm.status[name].Type = dm.tunnelType
		dm.status[name].Domain = dm.tunnelDomain
	}
	
	// 启动健康检查
	dm.startHealthCheck(name)
	
	// 监控进程退出
	go dm.monitorProcessExit(name)
	
	dm.logger().Infof("进程 %s 已启动 (PID: %d)", name, cmd.Process.Pid)
	return nil
}

func (dm *DaemonManager) logger() *logrus.Logger {
	return logrus.StandardLogger()
}

func (dm *DaemonManager) monitorProcessExit(name string) {
	dm.mu.RLock()
	cmd, exists := dm.processes[name]
	dm.mu.RUnlock()
	
	if !exists || cmd == nil {
		return
	}
	
	err := cmd.Wait()
	
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	if status, exists := dm.status[name]; exists {
		status.Running = false
		status.LastExit = time.Now()
	}
	
	if err != nil {
		dm.logger().Errorf("进程 %s 异常退出: %v", name, err)
		if status, exists := dm.status[name]; exists {
			status.Retries++
			if status.Retries <= dm.config.DaemonMaxRetries {
				dm.scheduleRestart(name)
			} else {
				dm.logger().Errorf("进程 %s 已达到最大重试次数", name)
			}
		}
	} else {
		dm.logger().Infof("进程 %s 正常退出", name)
	}
}

func (dm *DaemonManager) scheduleRestart(name string) {
	// 清除现有定时器
	if timer, exists := dm.restartTimers[name]; exists {
		timer.Stop()
	}
	
	dm.mu.RLock()
	status := dm.status[name]
	dm.mu.RUnlock()
	
	if status == nil {
		return
	}
	
	// 指数退避
	delay := time.Duration(dm.config.DaemonRestartDelay) * time.Millisecond *
		time.Duration(1<<uint(status.Retries-1))
	
	if delay > 60*time.Second {
		delay = 60 * time.Second
	}
	
	dm.logger().Infof("计划在 %v 后重启进程 %s (尝试 %d/%d)", 
		delay, name, status.Retries, dm.config.DaemonMaxRetries)
	
	timer := time.AfterFunc(delay, func() {
		dm.logger().Infof("正在重启进程 %s...", name)
		// 重启逻辑需要根据进程类型实现
	})
	
	dm.restartTimers[name] = timer
}

func (dm *DaemonManager) startHealthCheck(name string) {
	// 清除现有定时器
	if timer, exists := dm.checkTimers[name]; exists {
		timer.Stop()
	}
	
	interval := time.Duration(dm.config.DaemonCheckInterval) * time.Millisecond
	timer := time.AfterFunc(interval, func() {
		dm.checkProcessHealth(name)
	})
	
	dm.checkTimers[name] = timer
}

func (dm *DaemonManager) checkProcessHealth(name string) {
	dm.mu.RLock()
	cmd := dm.processes[name]
	status := dm.status[name]
	dm.mu.RUnlock()
	
	if cmd == nil || cmd.Process == nil {
		if status != nil {
			status.Running = false
		}
		return
	}
	
	// 检查进程是否存在
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		dm.mu.Lock()
		if status != nil {
			status.Running = false
		}
		dm.mu.Unlock()
		dm.logger().Warnf("进程 %s 健康检查失败: %v", name, err)
		
		// 触发重启
		if status != nil && status.Retries <= dm.config.DaemonMaxRetries {
			dm.scheduleRestart(name)
		}
	} else {
		dm.mu.Lock()
		if status != nil {
			status.Running = true
		}
		dm.mu.Unlock()
	}
	
	// 重新安排下次检查
	dm.startHealthCheck(name)
}

func (dm *DaemonManager) SetTunnelInfo(tunnelType, domain string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	dm.tunnelType = tunnelType
	dm.tunnelDomain = domain
	
	if status, exists := dm.status["tunnel"]; exists {
		status.Type = tunnelType
		status.Domain = domain
	}
}

func (dm *DaemonManager) GetStatus() map[string]interface{} {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	
	result := make(map[string]interface{})
	for name, status := range dm.status {
		result[name] = map[string]interface{}{
			"running":    status.Running,
			"retries":    status.Retries,
			"last_start": status.LastStart.Format(time.RFC3339),
			"last_exit":  status.LastExit.Format(time.RFC3339),
			"pid":        status.PID,
			"type":       status.Type,
			"domain":     status.Domain,
			"name":       status.Name,
		}
	}
	
	result["tunnel_info"] = map[string]interface{}{
		"type":   dm.tunnelType,
		"domain": dm.tunnelDomain,
	}
	
	result["config"] = map[string]interface{}{
		"check_interval": dm.config.DaemonCheckInterval,
		"max_retries":    dm.config.DaemonMaxRetries,
		"restart_delay":  dm.config.DaemonRestartDelay,
	}
	
	result["timestamp"] = time.Now().Format(time.RFC3339)
	result["uptime"] = int64(time.Since(startTime).Seconds())
	
	return result
}

func (dm *DaemonManager) RestartProcess(process string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	// 查找进程
	cmd, exists := dm.processes[process]
	if !exists {
		return fmt.Errorf("进程 %s 不存在", process)
	}
	
	// 停止进程
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
	
	// 清除定时器
	if timer, exists := dm.checkTimers[process]; exists {
		timer.Stop()
		delete(dm.checkTimers, process)
	}
	if timer, exists := dm.restartTimers[process]; exists {
		timer.Stop()
		delete(dm.restartTimers, process)
	}
	
	// 重置状态
	if status, exists := dm.status[process]; exists {
		status.Running = false
		status.Retries = 0
	}
	
	return nil
}

func (dm *DaemonManager) Cleanup() {
	dm.logger().Info("正在清理守护进程...")
	
	// 取消上下文
	dm.cancel()
	
	// 停止所有定时器
	for name, timer := range dm.checkTimers {
		timer.Stop()
		delete(dm.checkTimers, name)
	}
	for name, timer := range dm.restartTimers {
		timer.Stop()
		delete(dm.restartTimers, name)
	}
	
	// 停止所有进程
	for name, cmd := range dm.processes {
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
			dm.logger().Infof("已停止进程 %s", name)
		}
		delete(dm.processes, name)
	}
	
	// 更新状态
	for name := range dm.status {
		if status, exists := dm.status[name]; exists {
			status.Running = false
		}
	}
	
	dm.wg.Wait()
	dm.logger().Info("守护进程清理完成")
}

// ==============================
// 服务器方法
// ==============================
var startTime = time.Now()

func (s *Server) setupRoutes() {
	// 静态文件服务（如果存在）
	if _, err := os.Stat("index.html"); err == nil {
		s.router.PathPrefix("/").Handler(http.FileServer(http.Dir(".")))
	}
	
	// API路由
	s.router.HandleFunc("/", s.handleRoot).Methods("GET")
	s.router.HandleFunc("/daemon-status", s.handleDaemonStatus).Methods("GET")
	s.router.HandleFunc("/restart/{process}", s.handleRestart).Methods("POST")
	s.router.HandleFunc("/"+s.config.SubPath, s.handleSubscription).Methods("GET")
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	indexPath := "index.html"
	if _, err := os.Stat(indexPath); err == nil {
		http.ServeFile(w, r, indexPath)
		return
	}
	
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
	<!DOCTYPE html>
	<html>
	<head>
		<title>Tunnel Server</title>
		<meta charset="utf-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<style>
			body { font-family: Arial, sans-serif; margin: 40px; line-height: 1.6; }
			.container { max-width: 800px; margin: 0 auto; }
			h1 { color: #333; }
			.status { background: #f4f4f4; padding: 20px; border-radius: 5px; margin: 20px 0; }
			.btn { display: inline-block; background: #007bff; color: white; padding: 10px 20px; text-decoration: none; border-radius: 5px; margin: 5px; }
			.btn:hover { background: #0056b3; }
			.process { margin: 10px 0; padding: 10px; border: 1px solid #ddd; border-radius: 3px; }
			.running { color: green; }
			.stopped { color: red; }
		</style>
	</head>
	<body>
		<div class="container">
			<h1>🏔️ Tunnel Server 隧道服务器</h1>
			<p>服务器运行时间: %s</p>
			
			<div class="status">
				<h2>📊 系统状态</h2>
				<p><a href="/daemon-status" class="btn">查看详细状态</a></p>
				<p><a href="/%s" class="btn">下载订阅</a></p>
				
				<h3>🔄 重启服务</h3>
				<p>
					<a href="javascript:restartProcess('nezha')" class="btn">重启哪吒</a>
					<a href="javascript:restartProcess('xray')" class="btn">重启Xray</a>
					<a href="javascript:restartProcess('tunnel')" class="btn">重启隧道</a>
					<a href="javascript:restartProcess('all')" class="btn">重启所有</a>
				</p>
			</div>
			
			<h2>📖 使用说明</h2>
			<ul>
				<li><strong>订阅地址:</strong> <code>%s</code></li>
				<li><strong>内部端口:</strong> %d</li>
				<li><strong>外部端口:</strong> %d</li>
				<li><strong>UUID:</strong> %s</li>
			</ul>
		</div>
		
		<script>
			function restartProcess(process) {
				fetch('/restart/' + process, { method: 'POST' })
					.then(response => response.json())
					.then(data => {
						alert(data.message || '重启命令已发送');
						setTimeout(() => location.reload(), 2000);
					})
					.catch(error => {
						alert('重启失败: ' + error);
					});
			}
			
			// 自动更新状态
			function updateStatus() {
				fetch('/daemon-status')
					.then(response => response.json())
					.then(data => {
						if (data.success) {
							console.log('状态更新:', data);
						}
					});
			}
			
			// 每30秒更新一次状态
			setInterval(updateStatus, 30000);
		</script>
	</body>
	</html>
	`, 
		formatDuration(time.Since(startTime)),
		s.config.SubPath,
		s.config.SubPath,
		s.config.Port,
		s.config.ExternalPort,
		s.config.UUID,
	)
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	
	if days > 0 {
		return fmt.Sprintf("%d天%d小时%d分钟", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%d小时%d分钟", hours, minutes)
	} else if minutes > 0 {
		return fmt.Sprintf("%d分钟%d秒", minutes, seconds)
	}
	return fmt.Sprintf("%d秒", seconds)
}

func (s *Server) handleDaemonStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	status := s.daemon.GetStatus()
	response := map[string]interface{}{
		"success": true,
		"data":    status,
		"message": "状态查询成功",
	}
	
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	process := vars["process"]
	
	validProcesses := []string{"nezha", "xray", "tunnel", "all"}
	isValid := false
	for _, p := range validProcesses {
		if p == process {
			isValid = true
			break
		}
	}
	
	if !isValid {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("无效的进程名，可用选项: %v", validProcesses),
		})
		return
	}
	
	if process == "all" {
		// 重启所有进程
		for _, p := range []string{"nezha", "xray", "tunnel"} {
			s.daemon.RestartProcess(p)
		}
	} else {
		s.daemon.RestartProcess(process)
	}
	
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("进程 %s 重启命令已发送", process),
	})
}

func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	subPath := filepath.Join(s.config.FilePath, "sub.txt")
	
	// 检查订阅文件是否存在
	if _, err := os.Stat(subPath); os.IsNotExist(err) {
		// 生成默认订阅
		domain := s.daemon.tunnelDomain
		if domain == "" {
			domain = "example.trycloudflare.com"
		}
		
		if err := s.generateSubscription(domain); err != nil {
			s.logger.Errorf("生成订阅失败: %v", err)
			http.Error(w, "订阅未就绪，请稍后重试", http.StatusServiceUnavailable)
			return
		}
	}
	
	data, err := os.ReadFile(subPath)
	if err != nil {
		http.Error(w, "读取订阅文件失败", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=subscription.txt")
	w.Write(data)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"uptime":    time.Since(startTime).String(),
	})
}

// ==============================
// 文件下载
// ==============================
func (s *Server) downloadFiles() error {
	// 确定架构
	arch := getSystemArchitecture()
	
	// 构建下载列表
	downloads := []struct {
		name string
		url  string
	}{
		{
			name: "xray",
			url:  fmt.Sprintf("https://%s.ssss.nyc.mn/web", arch),
		},
		{
			name: "cloudflared",
			url:  fmt.Sprintf("https://%s.ssss.nyc.mn/bot", arch),
		},
	}
	
	// 添加哪吒代理
	if s.config.NezhaServer != "" && s.config.NezhaKey != "" {
		if s.config.NezhaPort != "" {
			downloads = append(downloads, struct {
				name string
				url  string
			}{
				name: "nezha-agent",
				url:  fmt.Sprintf("https://%s.ssss.nyc.mn/agent", arch),
			})
		} else {
			downloads = append(downloads, struct {
				name string
				url  string
			}{
				name: "nezha-php",
				url:  fmt.Sprintf("https://%s.ssss.nyc.mn/v1", arch),
			})
		}
	}
	
	// 下载所有文件
	for _, dl := range downloads {
		filePath := filepath.Join(s.config.FilePath, dl.name)
		
		// 如果文件已存在，跳过
		if _, err := os.Stat(filePath); err == nil {
			s.logger.Infof("文件已存在: %s", dl.name)
			continue
		}
		
		s.logger.Infof("正在下载: %s", dl.name)
		
		resp, err := http.Get(dl.url)
		if err != nil {
			return fmt.Errorf("下载 %s 失败: %v", dl.name, err)
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("下载 %s 失败: HTTP %d", dl.name, resp.StatusCode)
		}
		
		out, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("创建文件失败: %v", err)
		}
		defer out.Close()
		
		if _, err := io.Copy(out, resp.Body); err != nil {
			return fmt.Errorf("写入文件失败: %v", err)
		}
		
		// 设置执行权限
		if err := os.Chmod(filePath, 0755); err != nil {
			return fmt.Errorf("设置权限失败: %v", err)
		}
		
		s.logger.Infof("下载完成: %s", dl.name)
	}
	
	return nil
}

func getSystemArchitecture() string {
	// 简化架构判断
	if strings.Contains(strings.ToLower(os.Getenv("GOARCH")), "arm") ||
	   strings.Contains(strings.ToLower(os.Getenv("GOARM")), "arm") {
		return "arm64"
	}
	return "amd64"
}

// ==============================
// 配置文件生成
// ==============================
func (s *Server) generateXrayConfig() error {
	config := map[string]interface{}{
		"log": map[string]interface{}{
			"access":   "/dev/null",
			"error":    "/dev/null",
			"loglevel": "warning",
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
						"id":   s.config.UUID,
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
						"id": s.config.UUID,
					}},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":   "tcp",
					"security":  "none",
					"tcpSettings": map[string]interface{}{
						"header": map[string]interface{}{
							"type": "none",
						},
					},
				},
			},
			{
				"port":     3003,
				"listen":   "127.0.0.1",
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{{
						"id":    s.config.UUID,
						"level": 0,
					}},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":  "ws",
					"security": "none",
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
						"id":     s.config.UUID,
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
						"password": s.config.UUID,
					}},
				},
				"streamSettings": map[string]interface{}{
					"network":  "ws",
					"security": "none",
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
	
	configPath := filepath.Join(s.config.FilePath, "config.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	
	return os.WriteFile(configPath, data, 0644)
}

// ==============================
// 服务启动
// ==============================
func (s *Server) startNezha() error {
	if s.config.NezhaServer == "" || s.config.NezhaKey == "" {
		s.logger.Info("哪吒监控未配置，跳过启动")
		return nil
	}
	
	var cmd *exec.Cmd
	args := []string{}
	
	if s.config.NezhaPort != "" {
		// 使用agent版本
		agentPath := filepath.Join(s.config.FilePath, "nezha-agent")
		args = []string{
			"-s", fmt.Sprintf("%s:%s", s.config.NezhaServer, s.config.NezhaPort),
			"-p", s.config.NezhaKey,
			"--disable-auto-update",
			"--report-delay", "4",
			"--skip-conn",
			"--skip-procs",
		}
		
		// 检查是否需要TLS
		tlsPorts := []string{"443", "8443", "2096", "2087", "2083", "2053"}
		for _, port := range tlsPorts {
			if port == s.config.NezhaPort {
				args = append(args, "--tls")
				break
			}
		}
		
		cmd = exec.Command(agentPath, args...)
	} else {
		// 使用php版本
		phpPath := filepath.Join(s.config.FilePath, "nezha-php")
		
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
tls: true
use_gitee_to_upgrade: false
use_ipv6_country_code: false
uuid: %s
`, s.config.NezhaKey, s.config.NezhaServer, s.config.UUID)
		
		configPath := filepath.Join(s.config.FilePath, "nezha_config.yaml")
		if err := os.WriteFile(configPath, []byte(configYaml), 0644); err != nil {
			return err
		}
		
		cmd = exec.Command(phpPath, "-c", configPath)
	}
	
	// 启动进程
	return s.daemon.StartProcess("nezha", cmd.Path, cmd.Args[1:], func(c *exec.Cmd) {
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
	})
}

func (s *Server) startXray() error {
	xrayPath := filepath.Join(s.config.FilePath, "xray")
	configPath := filepath.Join(s.config.FilePath, "config.json")
	
	cmd := exec.Command(xrayPath, "-c", configPath)
	return s.daemon.StartProcess("xray", cmd.Path, cmd.Args[1:], func(c *exec.Cmd) {
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
	})
}

func (s *Server) startTunnel() error {
	tunnelType := s.analyzeTunnelType()
	s.daemon.SetTunnelInfo(tunnelType, s.config.ArgoDomain)
	
	cloudflaredPath := filepath.Join(s.config.FilePath, "cloudflared")
	var args []string
	
	switch tunnelType {
	case TunnelTypeFixed:
		// 固定隧道
		if err := s.prepareFixedTunnel(); err != nil {
			return err
		}
		configPath := filepath.Join(s.config.FilePath, "tunnel.yml")
		args = []string{
			"tunnel",
			"--edge-ip-version", "auto",
			"--config", configPath,
			"run",
		}
		
	case TunnelTypeToken:
		// Token隧道
		args = []string{
			"tunnel",
			"--edge-ip-version", "auto",
			"--no-autoupdate",
			"--protocol", "http2",
			"run",
			"--token", s.config.ArgoAuth,
		}
		
		if s.config.ArgoDomain != "" {
			args = append(args, "--hostname", s.config.ArgoDomain)
		}
		
	default:
		// 临时隧道
		logPath := filepath.Join(s.config.FilePath, "boot.log")
		args = []string{
			"tunnel",
			"--edge-ip-version", "auto",
			"--no-autoupdate",
			"--protocol", "http2",
			"--logfile", logPath,
			"--loglevel", "info",
			"run",
			"--url", fmt.Sprintf("http://localhost:%d", s.config.ExternalPort),
		}
	}
	
	cmd := exec.Command(cloudflaredPath, args...)
	return s.daemon.StartProcess("tunnel", cmd.Path, cmd.Args[1:], func(c *exec.Cmd) {
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
	})
}

func (s *Server) analyzeTunnelType() string {
	if s.config.ArgoAuth == "" {
		return TunnelTypeTemporary
	}
	
	// 检查是否是JSON配置
	if strings.Contains(s.config.ArgoAuth, "TunnelSecret") {
		return TunnelTypeFixed
	}
	
	// 检查是否是Token
	tokenPattern := `^[A-Z0-9a-z=]{120,250}$`
	if matched, _ := regexp.MatchString(tokenPattern, s.config.ArgoAuth); matched {
		return TunnelTypeToken
	}
	
	return TunnelTypeTemporary
}

func (s *Server) prepareFixedTunnel() error {
	var authData map[string]interface{}
	if err := json.Unmarshal([]byte(s.config.ArgoAuth), &authData); err != nil {
		return fmt.Errorf("解析Argo认证失败: %v", err)
	}
	
	tunnelID, ok := authData["TunnelID"].(string)
	if !ok {
		return fmt.Errorf("无效的隧道配置")
	}
	
	// 保存tunnel.json
	tunnelJSONPath := filepath.Join(s.config.FilePath, "tunnel.json")
	if err := os.WriteFile(tunnelJSONPath, []byte(s.config.ArgoAuth), 0644); err != nil {
		return err
	}
	
	// 生成tunnel.yml
	tunnelYAML := fmt.Sprintf(`
tunnel: %s
credentials-file: %s
protocol: http2

ingress:
  - hostname: %s
    service: http://localhost:%d
    originRequest:
      noTLSVerify: true
  - service: http_status:404
`, tunnelID, tunnelJSONPath, s.config.ArgoDomain, s.config.ExternalPort)
	
	tunnelYAMLPath := filepath.Join(s.config.FilePath, "tunnel.yml")
	return os.WriteFile(tunnelYAMLPath, []byte(tunnelYAML), 0644)
}

// ==============================
// 订阅生成
// ==============================
func (s *Server) generateSubscription(domain string) error {
	if domain == "" {
		return fmt.Errorf("隧道域名为空")
	}
	
	// 节点名称
	nodeName := s.config.Name
	if nodeName == "" {
		nodeName = "TunnelNode"
	}
	
	// Vless配置
	vlessConfig := fmt.Sprintf("vless://%s@%s:%d?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Fvless-argo%%3Fed%%3D2560#%s",
		s.config.UUID, s.config.CfIP, s.config.CfPort, domain, domain, nodeName)
	
	// Vmess配置
	vmessConfig := map[string]interface{}{
		"v":    "2",
		"ps":   nodeName,
		"add":  s.config.CfIP,
		"port": s.config.CfPort,
		"id":   s.config.UUID,
		"aid":  "0",
		"scy":  "none",
		"net":  "ws",
		"type": "none",
		"host": domain,
		"path": "/vmess-argo?ed=2560",
		"tls":  "tls",
		"sni":  domain,
		"fp":   "firefox",
	}
	vmessJSON, _ := json.Marshal(vmessConfig)
	vmessConfigStr := "vmess://" + base64.StdEncoding.EncodeToString(vmessJSON)
	
	// Trojan配置
	trojanConfig := fmt.Sprintf("trojan://%s@%s:%d?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Ftrojan-argo%%3Fed%%3D2560#%s",
		s.config.UUID, s.config.CfIP, s.config.CfPort, domain, domain, nodeName)
	
	// 组合订阅
	subscription := fmt.Sprintf("%s\n%s\n%s", vlessConfig, vmessConfigStr, trojanConfig)
	encoded := base64.StdEncoding.EncodeToString([]byte(subscription))
	
	// 保存订阅文件
	subPath := filepath.Join(s.config.FilePath, "sub.txt")
	return os.WriteFile(subPath, []byte(encoded), 0644)
}

// ==============================
// 隧道域名监控
// ==============================
func (s *Server) monitorTunnelDomain() {
	s.logger.Info("开始监控隧道域名...")
	
	// 等待隧道启动
	time.Sleep(10 * time.Second)
	
	for attempt := 1; attempt <= 10; attempt++ {
		domain := s.extractTunnelDomain()
		if domain != "" {
			s.logger.Infof("检测到隧道域名: %s (尝试 %d/10)", domain, attempt)
			s.daemon.SetTunnelInfo(s.daemon.tunnelType, domain)
			
			// 生成订阅
			if err := s.generateSubscription(domain); err != nil {
				s.logger.Errorf("生成订阅失败: %v", err)
			} else {
				s.logger.Info("订阅生成成功")
			}
			
			return
		}
		
		s.logger.Debugf("等待隧道域名... (尝试 %d/10)", attempt)
		time.Sleep(5 * time.Second)
	}
	
	s.logger.Warn("无法获取隧道域名，使用默认配置")
}

func (s *Server) extractTunnelDomain() string {
	logPath := filepath.Join(s.config.FilePath, "boot.log")
	
	// 检查日志文件是否存在
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return ""
	}
	
	// 读取日志文件
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	
	// 正则匹配域名
	re := regexp.MustCompile(`https?://([a-zA-Z0-9.-]+\.trycloudflare\.com)`)
	matches := re.FindStringSubmatch(string(data))
	if len(matches) > 1 {
		return matches[1]
	}
	
	return ""
}

// ==============================
// 启动HTTP服务器
// ==============================
func (s *Server) startHTTPServer() error {
	addr := fmt.Sprintf(":%d", s.config.Port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}
	
	s.logger.Infof("HTTP服务器启动在端口 %d", s.config.Port)
	
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Fatalf("HTTP服务器启动失败: %v", err)
		}
	}()
	
	return nil
}

// ==============================
// 启动代理服务器
// ==============================
func (s *Server) startProxyServer() error {
	// 创建代理处理函数
	proxyHandler := func(w http.ResponseWriter, r *http.Request) {
		// 判断请求路径
		path := r.URL.Path
		var targetURL string
		
		if strings.HasPrefix(path, "/vless-argo") || 
		   strings.HasPrefix(path, "/vmess-argo") || 
		   strings.HasPrefix(path, "/trojan-argo") ||
		   path == "/vless" || 
		   path == "/vmess" || 
		   path == "/trojan" {
			// 转发到Xray
			targetURL = "http://localhost:3001"
		} else {
			// 转发到主HTTP服务器
			targetURL = fmt.Sprintf("http://localhost:%d", s.config.Port)
		}
		
		// 创建代理
		target, _ := url.Parse(targetURL)
		proxy := httputil.NewSingleHostReverseProxy(target)
		
		// WebSocket支持
		if websocket.IsWebSocketUpgrade(r) {
			proxy.UpgradeHandler = func(resp *http.Response, conn *websocket.Conn, req *http.Request, ctx context.Context) error {
				return nil
			}
		}
		
		// 修改请求
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Header.Set("X-Forwarded-Host", r.Header.Get("Host"))
		r.Host = target.Host
		
		proxy.ServeHTTP(w, r)
	}
	
	// 创建HTTP服务器
	addr := fmt.Sprintf(":%d", s.config.ExternalPort)
	s.proxyServer = &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(proxyHandler),
	}
	
	s.logger.Infof("代理服务器启动在端口 %d", s.config.ExternalPort)
	
	go func() {
		if err := s.proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Fatalf("代理服务器启动失败: %v", err)
		}
	}()
	
	return nil
}

// ==============================
// 主启动函数
// ==============================
func (s *Server) Start() error {
	s.logger.Info("🚀 开始启动隧道服务器...")
	s.logger.Infof("配置文件路径: %s", s.config.FilePath)
	s.logger.Infof("UUID: %s", s.config.UUID)
	
	// 1. 清理旧文件
	s.cleanupOldFiles()
	
	// 2. 下载必要文件
	s.logger.Info("📥 正在下载必要文件...")
	if err := s.downloadFiles(); err != nil {
		return fmt.Errorf("下载文件失败: %v", err)
	}
	
	// 3. 生成Xray配置
	s.logger.Info("⚙️  正在生成Xray配置...")
	if err := s.generateXrayConfig(); err != nil {
		return fmt.Errorf("生成配置失败: %v", err)
	}
	
	// 4. 启动所有服务
	s.logger.Info("🚀 正在启动服务...")
	
	// 启动哪吒监控
	if s.config.NezhaServer != "" && s.config.NezhaKey != "" {
		s.logger.Info("🔧 正在启动哪吒监控...")
		if err := s.startNezha(); err != nil {
			s.logger.Errorf("启动哪吒监控失败: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
	
	// 启动Xray
	s.logger.Info("🛡️  正在启动Xray...")
	if err := s.startXray(); err != nil {
		s.logger.Errorf("启动Xray失败: %v", err)
	}
	time.Sleep(2 * time.Second)
	
	// 启动隧道
	s.logger.Info("🌉 正在启动隧道...")
	if err := s.startTunnel(); err != nil {
		s.logger.Errorf("启动隧道失败: %v", err)
	}
	
	// 5. 启动HTTP服务器
	s.logger.Info("🌐 正在启动HTTP服务器...")
	if err := s.startHTTPServer(); err != nil {
		return fmt.Errorf("启动HTTP服务器失败: %v", err)
	}
	
	// 6. 启动代理服务器
	s.logger.Info("🔄 正在启动代理服务器...")
	if err := s.startProxyServer(); err != nil {
		return fmt.Errorf("启动代理服务器失败: %v", err)
	}
	
	// 7. 监控隧道域名
	go s.monitorTunnelDomain()
	
	// 8. 自动访问任务
	if s.config.AutoAccess && s.config.ProjectURL != "" {
		go s.addAutoAccessTask()
	}
	
	s.logger.Info("✅ 服务器启动完成!")
	s.logger.Info("==========================================")
	s.logger.Infof("📊 控制面板: http://localhost:%d", s.config.Port)
	s.logger.Infof("🔗 订阅地址: http://localhost:%d/%s", s.config.Port, s.config.SubPath)
	s.logger.Infof("📈 状态监控: http://localhost:%d/daemon-status", s.config.Port)
	s.logger.Info("==========================================")
	
	return nil
}

func (s *Server) cleanupOldFiles() {
	// 清理旧文件，保留必要文件
	files, err := os.ReadDir(s.config.FilePath)
	if err != nil {
		return
	}
	
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		
		// 保留重要文件
		filename := file.Name()
		if filename == "daemon_status.json" || 
		   filename == "sub.txt" || 
		   strings.HasSuffix(filename, ".yaml") ||
		   strings.HasSuffix(filename, ".yml") ||
		   strings.HasSuffix(filename, ".json") {
			continue
		}
		
		// 删除其他文件
		filePath := filepath.Join(s.config.FilePath, filename)
		os.Remove(filePath)
	}
}

func (s *Server) addAutoAccessTask() {
	// 简单的自动访问实现
	if s.config.ProjectURL == "" {
		return
	}
	
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			resp, err := http.Get(s.config.ProjectURL)
			if err != nil {
				s.logger.Errorf("自动访问失败: %v", err)
			} else {
				resp.Body.Close()
				s.logger.Debugf("自动访问成功: %s", s.config.ProjectURL)
			}
		}
	}
}

// ==============================
// 优雅关闭
// ==============================
func (s *Server) Shutdown() {
	s.logger.Info("正在关闭服务器...")
	
	// 关闭HTTP服务器
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}
	
	// 关闭代理服务器
	if s.proxyServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.proxyServer.Shutdown(ctx)
	}
	
	// 清理守护进程
	s.daemon.Cleanup()
	
	s.logger.Info("服务器已关闭")
}

// ==============================
// 主函数
// ==============================
func main() {
	// 设置日志
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})
	logrus.SetLevel(logrus.InfoLevel)
	
	// 创建服务器
	server, err := NewServer()
	if err != nil {
		logrus.Fatalf("创建服务器失败: %v", err)
	}
	
	// 捕获中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// 启动服务器
	go func() {
		if err := server.Start(); err != nil {
			logrus.Fatalf("启动服务器失败: %v", err)
		}
	}()
	
	// 等待中断信号
	sig := <-sigChan
	logrus.Infof("收到信号: %v，正在关闭...", sig)
	
	// 优雅关闭
	server.Shutdown()
	
	logrus.Info("服务器已停止")
}
