package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
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
	UploadURL    string `json:"upload_url"`
	ProjectURL   string `json:"project_url"`
	AutoAccess   bool   `json:"auto_access"`
	FilePath     string `json:"file_path"`
	SubPath      string `json:"sub_path"`
	Port         int    `json:"port"`
	ExternalPort int    `json:"external_port"`
	UUID         string `json:"uuid"`
	NezhaServer  string `json:"nezha_server"`
	NezhaPort    string `json:"nezha_port"`
	NezhaKey     string `json:"nezha_key"`
	ArgoDomain   string `json:"argo_domain"`
	ArgoAuth     string `json:"argo_auth"`
	ArgoPort     int    `json:"argo_port"`
	CfIP         string `json:"cf_ip"`
	CfPort       int    `json:"cf_port"`
	Name         string `json:"name"`

	DaemonCheckInterval int `json:"daemon_check_interval"`
	DaemonMaxRetries    int `json:"daemon_max_retries"`
	DaemonRestartDelay  int `json:"daemon_restart_delay"`
}

// ==============================
// 进程状态
// ==============================
type ProcessStatus struct {
	Running   bool      `json:"running"`
	Retries   int       `json:"retries"`
	LastStart time.Time `json:"last_start"`
	LastExit  time.Time `json:"last_exit,omitempty"`
	PID       int       `json:"pid,omitempty"`
	Type      string    `json:"type,omitempty"`
	Domain    string    `json:"domain,omitempty"`
	Name      string    `json:"name,omitempty"`
}

// ==============================
// 隧道类型常量
// ==============================
const (
	TunnelTypeFixed     = "fixed"
	TunnelTypeToken     = "token"
	TunnelTypeTemporary = "temporary"
)

// ==============================
// 守护进程管理器
// ==============================
type DaemonManager struct {
	config      *Config
	processes   map[string]*exec.Cmd
	status      map[string]*ProcessStatus
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	checkTimers map[string]*time.Timer

	tunnelType   string
	tunnelDomain string
}

// ==============================
// 服务器实例
// ==============================
type Server struct {
	config      *Config
	daemon      *DaemonManager
	router      *mux.Router
	logger      *logrus.Logger
	httpServer  *http.Server
	proxyServer *http.Server
	upgrader    websocket.Upgrader
}

// ==============================
// 环境变量处理
// ==============================
func loadConfig() *Config {
	config := &Config{
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
		config.FilePath = val
	}
	if val := os.Getenv("SUB_PATH"); val != "" {
		config.SubPath = val
	}
	if val := os.Getenv("PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.Port = port
		}
	}
	if val := os.Getenv("EXTERNAL_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.ExternalPort = port
		}
	}
	if val := os.Getenv("UUID"); val != "" {
		config.UUID = val
	}
	if val := os.Getenv("UPLOAD_URL"); val != "" {
		config.UploadURL = val
	}
	if val := os.Getenv("PROJECT_URL"); val != "" {
		config.ProjectURL = val
	}
	if val := os.Getenv("AUTO_ACCESS"); val != "" {
		if auto, err := strconv.ParseBool(val); err == nil {
			config.AutoAccess = auto
		}
	}
	if val := os.Getenv("NEZHA_SERVER"); val != "" {
		config.NezhaServer = val
	}
	if val := os.Getenv("NEZHA_PORT"); val != "" {
		config.NezhaPort = val
	}
	if val := os.Getenv("NEZHA_KEY"); val != "" {
		config.NezhaKey = val
	}
	if val := os.Getenv("ARGO_DOMAIN"); val != "" {
		config.ArgoDomain = val
	}
	if val := os.Getenv("ARGO_AUTH"); val != "" {
		config.ArgoAuth = val
	}
	if val := os.Getenv("ARGO_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.ArgoPort = port
		}
	}
	if val := os.Getenv("CFIP"); val != "" {
		config.CfIP = val
	}
	if val := os.Getenv("CFPORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.CfPort = port
		}
	}
	if val := os.Getenv("NAME"); val != "" {
		config.Name = val
	}
	if val := os.Getenv("DAEMON_CHECK_INTERVAL"); val != "" {
		if interval, err := strconv.Atoi(val); err == nil {
			config.DaemonCheckInterval = interval
		}
	}
	if val := os.Getenv("DAEMON_MAX_RETRIES"); val != "" {
		if retries, err := strconv.Atoi(val); err == nil {
			config.DaemonMaxRetries = retries
		}
	}
	if val := os.Getenv("DAEMON_RESTART_DELAY"); val != "" {
		if delay, err := strconv.Atoi(val); err == nil {
			config.DaemonRestartDelay = delay
		}
	}

	return config
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
		config:      cfg,
		processes:   make(map[string]*exec.Cmd),
		status:      make(map[string]*ProcessStatus),
		ctx:         ctx,
		cancel:      cancel,
		checkTimers: make(map[string]*time.Timer),
	}
}

func (dm *DaemonManager) StartProcess(name, command string, args []string, options ...func(*exec.Cmd)) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// 创建命令
	cmd := exec.Command(command, args...)

	// 应用选项
	for _, option := range options {
		option(cmd)
	}

	// 设置默认输出
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}

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

	// 监控进程退出
	go func() {
		err := cmd.Wait()
		dm.mu.Lock()
		dm.status[name].Running = false
		dm.status[name].LastExit = time.Now()
		dm.mu.Unlock()

		if err != nil {
			logrus.Errorf("进程 %s 退出: %v", name, err)
			// 重启逻辑
			dm.scheduleRestart(name, command, args, options)
		}
	}()

	logrus.Infof("进程 %s 已启动 (PID: %d)", name, cmd.Process.Pid)
	return nil
}

func (dm *DaemonManager) scheduleRestart(name, command string, args []string, options []func(*exec.Cmd)) {
	dm.mu.RLock()
	status := dm.status[name]
	dm.mu.RUnlock()

	if status == nil {
		return
	}

	if status.Retries >= dm.config.DaemonMaxRetries {
		logrus.Errorf("进程 %s 已达到最大重试次数", name)
		return
	}

	delay := time.Duration(dm.config.DaemonRestartDelay) * time.Millisecond *
		time.Duration(1<<uint(status.Retries))

	if delay > 60*time.Second {
		delay = 60 * time.Second
	}

	logrus.Infof("计划在 %v 后重启 %s (尝试 %d)", delay, name, status.Retries+1)

	time.AfterFunc(delay, func() {
		dm.mu.Lock()
		dm.status[name].Retries++
		dm.mu.Unlock()

		logrus.Infof("重启进程 %s...", name)
		dm.StartProcess(name, command, args, options...)
	})
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

	result["timestamp"] = time.Now().Format(time.RFC3339)
	return result
}

func (dm *DaemonManager) RestartProcess(name string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// 停止进程
	if cmd, exists := dm.processes[name]; exists && cmd.Process != nil {
		cmd.Process.Kill()
	}

	// 重置状态
	if status, exists := dm.status[name]; exists {
		status.Running = false
		status.Retries = 0
	}

	return nil
}

func (dm *DaemonManager) Cleanup() {
	dm.cancel()

	dm.mu.Lock()
	defer dm.mu.Unlock()

	// 停止所有定时器
	for _, timer := range dm.checkTimers {
		timer.Stop()
	}

	// 停止所有进程
	for name, cmd := range dm.processes {
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
		}
		delete(dm.processes, name)
	}

	// 更新状态
	for _, status := range dm.status {
		status.Running = false
	}

	logrus.Info("守护进程清理完成")
}

// ==============================
// 服务器方法
// ==============================
var startTime = time.Now()

func NewServer() (*Server, error) {
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
	dm := NewDaemonManager(cfg)

	// 创建服务器
	s := &Server{
		config: cfg,
		daemon: dm,
		router: mux.NewRouter(),
		logger: logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	// 设置路由
	s.setupRoutes()

	return s, nil
}

func (s *Server) setupRoutes() {
	// 静态文件服务
	s.router.PathPrefix("/").Handler(http.FileServer(http.Dir(".")))

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
		</style>
	</head>
	<body>
		<div class="container">
			<h1>🏔️ Go Tunnel Server</h1>
			<p>服务器运行时间: %s</p>
			
			<div class="status">
				<h2>📊 系统状态</h2>
				<p><a href="/daemon-status" class="btn">查看详细状态</a></p>
				<p><a href="/%s" class="btn">下载订阅</a></p>
				
				<h3>🔄 重启服务</h3>
				<p>
					<a href="javascript:restartProcess('all')" class="btn">重启所有服务</a>
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
					})
					.catch(error => {
						alert('重启失败: ' + error);
					});
			}
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
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%d小时%d分钟%d秒", hours, minutes, seconds)
	}
	if minutes > 0 {
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
	w.Write(data)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
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
	// 简化版本，不实际下载文件
	s.logger.Info("跳过文件下载（简化版本）")
	return nil
}

// ==============================
// 配置文件生成
// ==============================
func (s *Server) generateXrayConfig() error {
	config := map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "warning",
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
				},
				"streamSettings": map[string]interface{}{
					"network": "tcp",
				},
			},
		},
		"outbounds": []map[string]interface{}{
			{
				"protocol": "freedom",
				"tag":      "direct",
			},
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

	s.logger.Info("启动哪吒监控（模拟）")
	return nil
}

func (s *Server) startXray() error {
	s.logger.Info("启动Xray（模拟）")
	return nil
}

func (s *Server) startTunnel() error {
	tunnelType := s.analyzeTunnelType()
	s.daemon.SetTunnelInfo(tunnelType, s.config.ArgoDomain)

	s.logger.Infof("启动隧道（类型: %s）", tunnelType)
	return nil
}

func (s *Server) analyzeTunnelType() string {
	if s.config.ArgoAuth == "" {
		return TunnelTypeTemporary
	}

	if strings.Contains(s.config.ArgoAuth, "TunnelSecret") {
		return TunnelTypeFixed
	}

	tokenPattern := `^[A-Z0-9a-z=]{120,250}$`
	if matched, _ := regexp.MatchString(tokenPattern, s.config.ArgoAuth); matched {
		return TunnelTypeToken
	}

	return TunnelTypeTemporary
}

// ==============================
// 订阅生成
// ==============================
func (s *Server) generateSubscription(domain string) error {
	if domain == "" {
		return fmt.Errorf("隧道域名为空")
	}

	nodeName := s.config.Name
	if nodeName == "" {
		nodeName = "GoTunnelNode"
	}

	vlessConfig := fmt.Sprintf("vless://%s@%s:%d?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Fvless-argo%%3Fed%%3D2560#%s",
		s.config.UUID, s.config.CfIP, s.config.CfPort, domain, domain, nodeName)

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

	trojanConfig := fmt.Sprintf("trojan://%s@%s:%d?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Ftrojan-argo%%3Fed%%3D2560#%s",
		s.config.UUID, s.config.CfIP, s.config.CfPort, domain, domain, nodeName)

	subscription := fmt.Sprintf("%s\n%s\n%s", vlessConfig, vmessConfigStr, trojanConfig)
	encoded := base64.StdEncoding.EncodeToString([]byte(subscription))

	subPath := filepath.Join(s.config.FilePath, "sub.txt")
	return os.WriteFile(subPath, []byte(encoded), 0644)
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
	proxyHandler := func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		var targetURL string

		if strings.HasPrefix(path, "/vless-argo") ||
			strings.HasPrefix(path, "/vmess-argo") ||
			strings.HasPrefix(path, "/trojan-argo") {
			targetURL = "http://localhost:3001"
		} else {
			targetURL = fmt.Sprintf("http://localhost:%d", s.config.Port)
		}

		target, _ := url.Parse(targetURL)
		proxy := httputil.NewSingleHostReverseProxy(target)

		// WebSocket支持
		if websocket.IsWebSocketUpgrade(r) {
			proxy.ServeHTTP(w, r)
			return
		}

		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Header.Set("X-Forwarded-Host", r.Header.Get("Host"))
		r.Host = target.Host

		proxy.ServeHTTP(w, r)
	}

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
	s.logger.Info("🚀 开始启动Go隧道服务器...")
	s.logger.Infof("配置文件路径: %s", s.config.FilePath)
	s.logger.Infof("UUID: %s", s.config.UUID)

	// 清理旧文件
	s.cleanupOldFiles()

	// 生成Xray配置
	s.logger.Info("⚙️ 正在生成Xray配置...")
	if err := s.generateXrayConfig(); err != nil {
		return fmt.Errorf("生成配置失败: %v", err)
	}

	// 启动所有服务
	s.logger.Info("🚀 正在启动服务...")

	// 启动哪吒监控
	if err := s.startNezha(); err != nil {
		s.logger.Errorf("启动哪吒监控失败: %v", err)
	}

	// 启动Xray
	if err := s.startXray(); err != nil {
		s.logger.Errorf("启动Xray失败: %v", err)
	}

	// 启动隧道
	if err := s.startTunnel(); err != nil {
		s.logger.Errorf("启动隧道失败: %v", err)
	}

	// 启动HTTP服务器
	s.logger.Info("🌐 正在启动HTTP服务器...")
	if err := s.startHTTPServer(); err != nil {
		return fmt.Errorf("启动HTTP服务器失败: %v", err)
	}

	// 启动代理服务器
	s.logger.Info("🔄 正在启动代理服务器...")
	if err := s.startProxyServer(); err != nil {
		return fmt.Errorf("启动代理服务器失败: %v", err)
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
	// 简化清理逻辑
	files, err := os.ReadDir(s.config.FilePath)
	if err != nil {
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		filePath := filepath.Join(s.config.FilePath, file.Name())
		os.Remove(filePath)
	}
}

// ==============================
// 优雅关闭
// ==============================
func (s *Server) Shutdown() {
	s.logger.Info("正在关闭服务器...")

	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}

	if s.proxyServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.proxyServer.Shutdown(ctx)
	}

	s.daemon.Cleanup()
	s.logger.Info("服务器已关闭")
}

// ==============================
// 初始化随机种子
// ==============================
func init() {
	rand.Seed(time.Now().UnixNano())
}

// ==============================
// 主函数
// ==============================
func main() {
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})
	logrus.SetLevel(logrus.InfoLevel)

	server, err := NewServer()
	if err != nil {
		logrus.Fatalf("创建服务器失败: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := server.Start(); err != nil {
			logrus.Fatalf("启动服务器失败: %v", err)
		}
	}()

	sig := <-sigChan
	logrus.Infof("收到信号: %v，正在关闭...", sig)

	server.Shutdown()
	logrus.Info("服务器已停止")
}
