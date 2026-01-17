package main

import (
	"bufio"
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
	"runtime"
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
	AppName        string `json:"app_name"`
	AppVersion     string `json:"app_version"`
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
	
	// 性能配置
	MaxConcurrentRequests int `json:"max_concurrent_requests"`
	RequestTimeout        int `json:"request_timeout"`
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
	Uptime     string    `json:"uptime,omitempty"`
	Memory     string    `json:"memory,omitempty"`
	CPU        string    `json:"cpu,omitempty"`
}

// ==============================
// 隧道类型常量
// ==============================
const (
	TunnelTypeFixed    = "fixed"
	TunnelTypeToken    = "token"
	TunnelTypeTemporary = "temporary"
	AppVersion         = "1.0.0"
	AppName            = "app-go"
)

// ==============================
// 守护进程管理器
// ==============================
type DaemonManager struct {
	config       *Config
	processes    map[string]*exec.Cmd
	status       map[string]*ProcessStatus
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	
	// 隧道信息
	tunnelType   string
	tunnelDomain string
	checkTimers  map[string]*time.Timer
	restartTimers map[string]*time.Timer
}

// ==============================
// 应用实例
// ==============================
type App struct {
	config      *Config
	daemon      *DaemonManager
	router      *mux.Router
	logger      *logrus.Logger
	httpServer  *http.Server
	proxyServer *http.Server
	startTime   time.Time
	metrics     *AppMetrics
}

// ==============================
// 应用指标
// ==============================
type AppMetrics struct {
	mu               sync.RWMutex
	TotalRequests    int64     `json:"total_requests"`
	ActiveConnections int64    `json:"active_connections"`
	Uptime           time.Duration `json:"uptime"`
	MemoryUsage      uint64    `json:"memory_usage"`
	CPUUsage         float64   `json:"cpu_usage"`
}

// ==============================
// API响应结构
// ==============================
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Version string      `json:"version"`
	Timestamp string    `json:"timestamp"`
}

// ==============================
// 初始化函数
// ==============================
func NewApp() (*App, error) {
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
	
	// 创建应用
	app := &App{
		config:    cfg,
		daemon:    dm,
		router:    mux.NewRouter(),
		logger:    logger,
		startTime: time.Now(),
		metrics: &AppMetrics{
			TotalRequests:    0,
			ActiveConnections: 0,
		},
	}
	
	// 设置路由器
	app.setupRoutes()
	
	return app, nil
}

// ==============================
// 环境变量处理
// ==============================
func loadConfig() *Config {
	cfg := &Config{
		AppName:    getEnv("APP_NAME", AppName),
		AppVersion: getEnv("APP_VERSION", AppVersion),
		FilePath:   getEnv("FILE_PATH", "./data"),
		SubPath:    getEnv("SUB_PATH", "sub"),
		Port:       getEnvAsInt("PORT", 3000),
		ExternalPort: getEnvAsInt("EXTERNAL_PORT", 7860),
		UUID:       getEnv("UUID", generateRandomUUID()),
		CfIP:       getEnv("CFIP", "cdn.example.com"),
		CfPort:     getEnvAsInt("CFPORT", 443),
		ArgoPort:   getEnvAsInt("ARGO_PORT", 7860),
		
		// 守护进程配置
		DaemonCheckInterval: getEnvAsInt("DAEMON_CHECK_INTERVAL", 30000),
		DaemonMaxRetries:    getEnvAsInt("DAEMON_MAX_RETRIES", 5),
		DaemonRestartDelay:  getEnvAsInt("DAEMON_RESTART_DELAY", 10000),
		
		// 性能配置
		MaxConcurrentRequests: getEnvAsInt("MAX_CONCURRENT_REQUESTS", 1000),
		RequestTimeout:        getEnvAsInt("REQUEST_TIMEOUT", 30),
	}
	
	// 其他环境变量
	cfg.UploadURL = os.Getenv("UPLOAD_URL")
	cfg.ProjectURL = os.Getenv("PROJECT_URL")
	cfg.AutoAccess = getEnvAsBool("AUTO_ACCESS", false)
	cfg.NezhaServer = os.Getenv("NEZHA_SERVER")
	cfg.NezhaPort = os.Getenv("NEZHA_PORT")
	cfg.NezhaKey = os.Getenv("NEZHA_KEY")
	cfg.ArgoDomain = os.Getenv("ARGO_DOMAIN")
	cfg.ArgoAuth = os.Getenv("ARGO_AUTH")
	cfg.Name = os.Getenv("NAME")
	
	return cfg
}

// 生成随机UUID
func generateRandomUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "35461c1b-c9fb-efd5-e5d4-cf754d37bd4b"
	}
	
	return fmt.Sprintf("%x-%x-%x-%x-%x", 
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

// ==============================
// HTTP中间件
// ==============================
func (a *App) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// 更新指标
		a.metrics.mu.Lock()
		a.metrics.TotalRequests++
		a.metrics.ActiveConnections++
		a.metrics.mu.Unlock()
		
		defer func() {
			a.metrics.mu.Lock()
			a.metrics.ActiveConnections--
			a.metrics.mu.Unlock()
			
			a.logger.WithFields(logrus.Fields{
				"method":   r.Method,
				"path":     r.URL.Path,
				"ip":       r.RemoteAddr,
				"duration": time.Since(start).String(),
			}).Info("HTTP请求")
		}()
		
		next.ServeHTTP(w, r)
	})
}

func (a *App) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		
		next.ServeHTTP(w, r)
	})
}

// ==============================
// HTTP路由设置
// ==============================
func (a *App) setupRoutes() {
	// 应用中间件
	a.router.Use(a.loggingMiddleware)
	a.router.Use(a.corsMiddleware)
	
	// 静态文件服务
	a.router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", 
		http.FileServer(http.Dir("./static"))))
	
	// API路由
	a.router.HandleFunc("/", a.handleRoot).Methods("GET")
	a.router.HandleFunc("/api/status", a.handleStatus).Methods("GET")
	a.router.HandleFunc("/api/daemon-status", a.handleDaemonStatus).Methods("GET")
	a.router.HandleFunc("/api/restart/{process}", a.handleRestart).Methods("POST")
	a.router.HandleFunc("/api/metrics", a.handleMetrics).Methods("GET")
	a.router.HandleFunc("/api/health", a.handleHealth).Methods("GET")
	a.router.HandleFunc("/api/version", a.handleVersion).Methods("GET")
	
	// 订阅路由
	a.router.HandleFunc("/"+a.config.SubPath, a.handleSubscription).Methods("GET")
	a.router.HandleFunc("/api/subscription", a.handleSubscriptionAPI).Methods("GET")
	
	// 配置路由
	a.router.HandleFunc("/api/config", a.handleConfig).Methods("GET")
	
	// 隧道管理
	a.router.HandleFunc("/api/tunnel/status", a.handleTunnelStatus).Methods("GET")
	a.router.HandleFunc("/api/tunnel/restart", a.handleTunnelRestart).Methods("POST")
	
	// 节点管理
	a.router.HandleFunc("/api/nodes", a.handleNodes).Methods("GET")
	
	// 文件上传（用于订阅）
	a.router.HandleFunc("/api/upload", a.handleUpload).Methods("POST")
	
	// WebSocket支持
	a.router.HandleFunc("/ws", a.handleWebSocket).Methods("GET")
}

// ==============================
// HTTP处理器
// ==============================
func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	
	html := fmt.Sprintf(`
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s v%s</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            margin: 20px;
            background: #f5f5f5;
        }
        .container {
            max-width: 800px;
            margin: 0 auto;
            background: white;
            padding: 20px;
            border-radius: 10px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        h1 {
            color: #333;
        }
        .status-item {
            margin: 10px 0;
            padding: 10px;
            border: 1px solid #ddd;
            border-radius: 5px;
        }
        .running { color: green; }
        .stopped { color: red; }
        .btn {
            display: inline-block;
            padding: 10px 20px;
            margin: 5px;
            background: #007bff;
            color: white;
            text-decoration: none;
            border-radius: 5px;
            cursor: pointer;
        }
        .btn:hover { background: #0056b3; }
    </style>
</head>
<body>
    <div class="container">
        <h1>%s v%s</h1>
        <p>隧道管理平台</p>
        
        <div id="status">
            <h2>系统状态</h2>
            <div id="process-status">加载中...</div>
        </div>
        
        <div style="margin-top: 20px;">
            <a href="/%s" class="btn">📥 订阅链接</a>
            <a href="/api/status" class="btn">📊 状态查看</a>
            <a href="/api/health" class="btn">🏥 健康检查</a>
        </div>
        
        <div style="margin-top: 20px;">
            <h3>订阅地址</h3>
            <input type="text" value="http://%s:%d/%s" 
                   style="width: 100%%; padding: 10px; border: 1px solid #ddd; border-radius: 5px;" 
                   readonly onclick="this.select()">
        </div>
    </div>
    
    <script>
        async function loadStatus() {
            try {
                const response = await fetch('/api/status');
                const data = await response.json();
                
                if (data.success) {
                    let html = '';
                    const processes = data.data.processes;
                    
                    for (const [name, status] of Object.entries(processes)) {
                        const statusClass = status.running ? 'running' : 'stopped';
                        const statusText = status.running ? '运行中' : '已停止';
                        
                        html += \`
                            <div class="status-item">
                                <strong>\${name}:</strong> 
                                <span class="\${statusClass}">\${statusText}</span>
                                <span style="margin-left: 20px;">PID: \${status.pid || 'N/A'}</span>
                            </div>
                        \`;
                    }
                    
                    document.getElementById('process-status').innerHTML = html;
                }
            } catch (error) {
                console.error('加载状态失败:', error);
                document.getElementById('process-status').innerHTML = '加载失败';
            }
        }
        
        // 初始加载
        loadStatus();
        // 每30秒刷新
        setInterval(loadStatus, 30000);
    </script>
</body>
</html>`, 
a.config.AppName, a.config.AppVersion,
a.config.AppName, a.config.AppVersion,
a.config.SubPath,
getServerIP(), a.config.Port, a.config.SubPath)
	
	w.Write([]byte(html))
}

func getServerIP() string {
	return "localhost"
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "系统状态",
		Data: map[string]interface{}{
			"version":   a.config.AppVersion,
			"name":      a.config.AppName,
			"uptime":    time.Since(a.startTime).Seconds(),
			"processes": a.daemon.GetStatus(),
			"metrics": map[string]interface{}{
				"total_requests":     a.metrics.TotalRequests,
				"active_connections": a.metrics.ActiveConnections,
			},
		},
		Version:   a.config.AppVersion,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (a *App) handleDaemonStatus(w http.ResponseWriter, r *http.Request) {
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "守护进程状态",
		Data:    a.daemon.GetStatus(),
		Version: a.config.AppVersion,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (a *App) handleRestart(w http.ResponseWriter, r *http.Request) {
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
		a.sendJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   fmt.Sprintf("无效的进程名，可用选项: %v", validProcesses),
			Version: a.config.AppVersion,
		})
		return
	}
	
	if process == "all" {
		// 重启所有进程
		for _, p := range []string{"nezha", "xray", "tunnel"} {
			a.daemon.RestartProcess(p)
		}
		a.sendJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: "所有进程重启命令已发送",
			Version: a.config.AppVersion,
		})
	} else {
		a.daemon.RestartProcess(process)
		a.sendJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: fmt.Sprintf("进程 %s 重启命令已发送", process),
			Version: a.config.AppVersion,
		})
	}
}

func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	a.metrics.mu.RLock()
	defer a.metrics.mu.RUnlock()
	
	data := map[string]interface{}{
		"total_requests":     a.metrics.TotalRequests,
		"active_connections": a.metrics.ActiveConnections,
		"uptime":            time.Since(a.startTime).String(),
		"memory_usage":      a.metrics.MemoryUsage,
		"cpu_usage":         a.metrics.CPUUsage,
		"processes":         len(a.daemon.status),
	}
	
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success:   true,
		Message:   "系统指标",
		Data:      data,
		Version:   a.config.AppVersion,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	// 检查所有进程的健康状态
	status := a.daemon.GetStatus()
	allHealthy := true
	
	for _, proc := range status {
		if running, ok := proc.(map[string]interface{})["running"].(bool); ok {
			if !running {
				allHealthy = false
				break
			}
		}
	}
	
	if allHealthy {
		a.sendJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: "所有服务运行正常",
			Data:    status,
			Version: a.config.AppVersion,
		})
	} else {
		a.sendJSON(w, http.StatusServiceUnavailable, APIResponse{
			Success: false,
			Error:   "部分服务不可用",
			Data:    status,
			Version: a.config.AppVersion,
		})
	}
}

func (a *App) handleVersion(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"name":        a.config.AppName,
		"version":     a.config.AppVersion,
		"build_time":  a.startTime.Format(time.RFC3339),
		"go_version":  runtime.Version(),
		"platform":    runtime.GOOS + "/" + runtime.GOARCH,
		"uptime":      time.Since(a.startTime).String(),
		"config_path": a.config.FilePath,
	}
	
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success:   true,
		Message:   "版本信息",
		Data:      info,
		Version:   a.config.AppVersion,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (a *App) handleSubscription(w http.ResponseWriter, r *http.Request) {
	domain := a.daemon.tunnelDomain
	if domain == "" {
		domain = "example.trycloudflare.com"
	}
	
	subscription := a.generateSubscription(domain)
	encoded := base64.StdEncoding.EncodeToString([]byte(subscription))
	
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=subscription.txt")
	w.Write([]byte(encoded))
}

func (a *App) handleSubscriptionAPI(w http.ResponseWriter, r *http.Request) {
	domain := a.daemon.tunnelDomain
	if domain == "" {
		domain = "example.trycloudflare.com"
	}
	
	subscription := a.generateSubscription(domain)
	
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "订阅信息",
		Data: map[string]interface{}{
			"subscription": subscription,
			"domain":      domain,
			"url":         fmt.Sprintf("http://%s:%d/%s", getServerIP(), a.config.Port, a.config.SubPath),
		},
		Version:   a.config.AppVersion,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	// 安全过滤敏感信息
	safeConfig := *a.config
	safeConfig.NezhaKey = "***"
	safeConfig.ArgoAuth = "***"
	safeConfig.UUID = "***"
	
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "配置信息（敏感信息已隐藏）",
		Data:    safeConfig,
		Version: a.config.AppVersion,
	})
}

func (a *App) handleTunnelStatus(w http.ResponseWriter, r *http.Request) {
	status := a.daemon.GetStatus()
	tunnelInfo, _ := status["tunnel_info"].(map[string]interface{})
	
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "隧道状态",
		Data: map[string]interface{}{
			"tunnel": map[string]interface{}{
				"type":   a.daemon.tunnelType,
				"domain": a.daemon.tunnelDomain,
				"running": func() bool {
					if s, ok := status["tunnel"]; ok {
						if m, ok := s.(map[string]interface{}); ok {
							if r, ok := m["running"].(bool); ok {
								return r
							}
						}
					}
					return false
				}(),
				"uptime": time.Since(a.startTime).Seconds(),
			},
			"tunnel_info": tunnelInfo,
		},
		Version:   a.config.AppVersion,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (a *App) handleTunnelRestart(w http.ResponseWriter, r *http.Request) {
	a.daemon.RestartProcess("tunnel")
	
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "隧道重启命令已发送",
		Version: a.config.AppVersion,
	})
}

func (a *App) handleNodes(w http.ResponseWriter, r *http.Request) {
	// 返回节点列表
	domain := a.daemon.tunnelDomain
	if domain == "" {
		domain = "example.trycloudflare.com"
	}
	
	nodes := a.generateNodeConfigs(domain)
	
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "节点列表",
		Data:    nodes,
		Version: a.config.AppVersion,
	})
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	// 文件上传处理
	if r.Method != "POST" {
		a.sendJSON(w, http.StatusMethodNotAllowed, APIResponse{
			Success: false,
			Error:   "Method not allowed",
			Version: a.config.AppVersion,
		})
		return
	}
	
	// 解析multipart表单
	err := r.ParseMultipartForm(10 << 20) // 10MB限制
	if err != nil {
		a.sendJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "解析表单失败: " + err.Error(),
			Version: a.config.AppVersion,
		})
		return
	}
	
	file, handler, err := r.FormFile("file")
	if err != nil {
		a.sendJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Error:   "获取文件失败: " + err.Error(),
			Version: a.config.AppVersion,
		})
		return
	}
	defer file.Close()
	
	// 保存文件
	filePath := filepath.Join(a.config.FilePath, handler.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		a.sendJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   "创建文件失败: " + err.Error(),
			Version: a.config.AppVersion,
		})
		return
	}
	defer dst.Close()
	
	if _, err := io.Copy(dst, file); err != nil {
		a.sendJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Error:   "保存文件失败: " + err.Error(),
			Version: a.config.AppVersion,
		})
		return
	}
	
	a.sendJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "文件上传成功",
		Data: map[string]interface{}{
			"filename": handler.Filename,
			"size":     handler.Size,
			"path":     filePath,
		},
		Version:   a.config.AppVersion,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (a *App) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // 允许所有来源
		},
	}
	
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		a.logger.Error("WebSocket升级失败:", err)
		return
	}
	defer conn.Close()
	
	// 发送欢迎消息
	conn.WriteJSON(map[string]interface{}{
		"type":    "welcome",
		"message": "Connected to app-go WebSocket",
		"time":    time.Now().Format(time.RFC3339),
	})
	
	// 定期发送状态更新
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// 发送状态更新
			status := a.daemon.GetStatus()
			conn.WriteJSON(map[string]interface{}{
				"type":   "status",
				"data":   status,
				"time":   time.Now().Format(time.RFC3339),
				"uptime": time.Since(a.startTime).Seconds(),
			})
			
		case <-a.daemon.ctx.Done():
			// 应用关闭
			conn.WriteJSON(map[string]interface{}{
				"type":    "shutdown",
				"message": "Server is shutting down",
				"time":    time.Now().Format(time.RFC3339),
			})
			return
		}
	}
}

// ==============================
// 辅助函数
// ==============================
func (a *App) sendJSON(w http.ResponseWriter, status int, response APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(response)
}

func (a *App) generateSubscription(domain string) string {
	nodeName := a.config.Name
	if nodeName == "" {
		nodeName = "AppGoNode"
	}
	
	// Vless配置
	vlessURL := fmt.Sprintf("vless://%s@%s:%d?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=/vless-argo#%s",
		a.config.UUID, a.config.CfIP, a.config.CfPort, domain, domain, nodeName)
	
	// Vmess配置
	vmessConfig := map[string]interface{}{
		"v":    "2",
		"ps":   nodeName,
		"add":  a.config.CfIP,
		"port": a.config.CfPort,
		"id":   a.config.UUID,
		"aid":  "0",
		"scy":  "none",
		"net":  "ws",
		"type": "none",
		"host": domain,
		"path": "/vmess-argo",
		"tls":  "tls",
		"sni":  domain,
		"fp":   "firefox",
	}
	
	vmessJSON, _ := json.Marshal(vmessConfig)
	vmessURL := "vmess://" + base64.StdEncoding.EncodeToString(vmessJSON)
	
	// Trojan配置
	trojanURL := fmt.Sprintf("trojan://%s@%s:%d?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=/trojan-argo#%s",
		a.config.UUID, a.config.CfIP, a.config.CfPort, domain, domain, nodeName)
	
	return fmt.Sprintf("%s\n%s\n%s", vlessURL, vmessURL, trojanURL)
}

func (a *App) generateNodeConfigs(domain string) map[string]interface{} {
	nodeName := a.config.Name
	if nodeName == "" {
		nodeName = "AppGoNode"
	}
	
	return map[string]interface{}{
		"vless": map[string]string{
			"url": fmt.Sprintf("vless://%s@%s:%d?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=/vless-argo#%s",
				a.config.UUID, a.config.CfIP, a.config.CfPort, domain, domain, nodeName),
		},
		"vmess": map[string]string{
			"url": "vmess://" + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(`
{
  "v": "2",
  "ps": "%s",
  "add": "%s",
  "port": "%d",
  "id": "%s",
  "aid": "0",
  "scy": "none",
  "net": "ws",
  "type": "none",
  "host": "%s",
  "path": "/vmess-argo",
  "tls": "tls",
  "sni": "%s",
  "fp": "firefox"
}`, nodeName, a.config.CfIP, a.config.CfPort, a.config.UUID, domain, domain))),
		},
		"trojan": map[string]string{
			"url": fmt.Sprintf("trojan://%s@%s:%d?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=/trojan-argo#%s",
				a.config.UUID, a.config.CfIP, a.config.CfPort, domain, domain, nodeName),
		},
	}
}

// ==============================
// 守护进程管理器方法
// ==============================
func (dm *DaemonManager) StartProcess(name, command string, args []string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	// 如果进程已存在，先停止
	if cmd, exists := dm.processes[name]; exists && cmd.Process != nil {
		cmd.Process.Kill()
	}
	
	// 创建命令
	cmd := exec.Command(command, args...)
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
	
	// 监控进程退出
	go dm.monitorProcessExit(name)
	
	log.Printf("进程 %s 已启动 (PID: %d)", name, cmd.Process.Pid)
	return nil
}

func (dm *DaemonManager) monitorProcessExit(name string) {
	cmd := dm.processes[name]
	err := cmd.Wait()
	
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	if status, exists := dm.status[name]; exists {
		status.Running = false
		status.LastExit = time.Now()
		
		if err != nil {
			log.Printf("进程 %s 异常退出: %v", name, err)
			status.Retries++
			if status.Retries <= dm.config.DaemonMaxRetries {
				dm.scheduleRestart(name)
			} else {
				log.Printf("进程 %s 已达到最大重试次数", name)
			}
		} else {
			log.Printf("进程 %s 正常退出", name)
		}
	}
}

func (dm *DaemonManager) scheduleRestart(name string) {
	delay := time.Duration(dm.config.DaemonRestartDelay) * time.Millisecond
	log.Printf("计划在 %v 后重启进程 %s", delay, name)
	
	time.AfterFunc(delay, func() {
		log.Printf("重启进程 %s...", name)
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
	
	// 重置状态
	if status, exists := dm.status[process]; exists {
		status.Running = false
		status.Retries = 0
	}
	
	return nil
}

func (dm *DaemonManager) Cleanup() {
	log.Println("正在清理守护进程...")
	
	// 停止所有进程
	for name, cmd := range dm.processes {
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
			log.Printf("已停止进程 %s", name)
		}
	}
	
	dm.cancel()
	log.Println("守护进程清理完成")
}

// ==============================
// 文件下载功能
// ==============================
func (a *App) downloadFiles() error {
	arch := "amd64"
	if strings.Contains(runtime.GOARCH, "arm") {
		arch = "arm64"
	}
	
	downloads := []struct {
		name string
		url  string
	}{
		{"xray", fmt.Sprintf("https://%s.ssss.nyc.mn/web", arch)},
		{"cloudflared", fmt.Sprintf("https://%s.ssss.nyc.mn/bot", arch)},
	}
	
	if a.config.NezhaServer != "" && a.config.NezhaKey != "" {
		if a.config.NezhaPort != "" {
			downloads = append(downloads, struct {
				name string
				url  string
			}{"nezha-agent", fmt.Sprintf("https://%s.ssss.nyc.mn/agent", arch)})
		} else {
			downloads = append(downloads, struct {
				name string
				url  string
			}{"nezha-php", fmt.Sprintf("https://%s.ssss.nyc.mn/v1", arch)})
		}
	}
	
	for _, dl := range downloads {
		filePath := filepath.Join(a.config.FilePath, dl.name)
		
		// 如果文件已存在，跳过
		if _, err := os.Stat(filePath); err == nil {
			a.logger.Infof("文件已存在: %s", dl.name)
			continue
		}
		
		a.logger.Infof("正在下载: %s", dl.name)
		
		resp, err := http.Get(dl.url)
		if err != nil {
			return fmt.Errorf("下载 %s 失败: %v", dl.name, err)
		}
		defer resp.Body.Close()
		
		out, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("创建文件失败: %v", err)
		}
		defer out.Close()
		
		_, err = io.Copy(out, resp.Body)
		if err != nil {
			return fmt.Errorf("写入文件失败: %v", err)
		}
		
		os.Chmod(filePath, 0755)
		a.logger.Infof("下载完成: %s", dl.name)
	}
	
	return nil
}

// ==============================
// 服务启动功能
// ==============================
func (a *App) startNezha() error {
	if a.config.NezhaServer == "" || a.config.NezhaKey == "" {
		a.logger.Info("哪吒监控未配置，跳过启动")
		return nil
	}
	
	var cmd *exec.Cmd
	
	if a.config.NezhaPort != "" {
		agentPath := filepath.Join(a.config.FilePath, "nezha-agent")
		args := []string{
			"-s", fmt.Sprintf("%s:%s", a.config.NezhaServer, a.config.NezhaPort),
			"-p", a.config.NezhaKey,
			"--disable-auto-update",
			"--skip-conn",
			"--skip-procs",
		}
		
		cmd = exec.Command(agentPath, args...)
	} else {
		phpPath := filepath.Join(a.config.FilePath, "nezha-php")
		configContent := fmt.Sprintf(`
client_secret: %s
server: %s
uuid: %s
`, a.config.NezhaKey, a.config.NezhaServer, a.config.UUID)
		
		configPath := filepath.Join(a.config.FilePath, "nezha_config.yaml")
		os.WriteFile(configPath, []byte(configContent), 0644)
		
		cmd = exec.Command(phpPath, "-c", configPath)
	}
	
	return a.daemon.StartProcess("nezha", cmd.Path, cmd.Args[1:])
}

func (a *App) startXray() error {
	xrayPath := filepath.Join(a.config.FilePath, "xray")
	configPath := filepath.Join(a.config.FilePath, "config.json")
	
	a.generateXrayConfig()
	
	cmd := exec.Command(xrayPath, "-c", configPath)
	return a.daemon.StartProcess("xray", cmd.Path, cmd.Args[1:])
}

func (a *App) startTunnel() error {
	tunnelType := a.analyzeTunnelType()
	a.daemon.SetTunnelInfo(tunnelType, a.config.ArgoDomain)
	
	cloudflaredPath := filepath.Join(a.config.FilePath, "cloudflared")
	var args []string
	
	switch tunnelType {
	case TunnelTypeFixed:
		a.prepareFixedTunnel()
		configPath := filepath.Join(a.config.FilePath, "tunnel.yml")
		args = []string{"tunnel", "--config", configPath, "run"}
	case TunnelTypeToken:
		args = []string{"tunnel", "run", "--token", a.config.ArgoAuth}
		if a.config.ArgoDomain != "" {
			args = append(args, "--hostname", a.config.ArgoDomain)
		}
	default:
		args = []string{"tunnel", "--url", fmt.Sprintf("http://localhost:%d", a.config.ExternalPort)}
	}
	
	cmd := exec.Command(cloudflaredPath, args...)
	return a.daemon.StartProcess("tunnel", cmd.Path, cmd.Args[1:])
}

func (a *App) analyzeTunnelType() string {
	if a.config.ArgoAuth == "" {
		return TunnelTypeTemporary
	}
	
	if strings.Contains(a.config.ArgoAuth, "TunnelSecret") {
		return TunnelTypeFixed
	}
	
	// 检查是否是Token
	tokenPattern := `^[A-Z0-9a-z=]{120,250}$`
	if matched, _ := regexp.MatchString(tokenPattern, a.config.ArgoAuth); matched {
		return TunnelTypeToken
	}
	
	return TunnelTypeTemporary
}

func (a *App) prepareFixedTunnel() error {
	var authData map[string]interface{}
	if err := json.Unmarshal([]byte(a.config.ArgoAuth), &authData); err != nil {
		return fmt.Errorf("解析Argo认证失败: %v", err)
	}
	
	tunnelID, ok := authData["TunnelID"].(string)
	if !ok {
		return fmt.Errorf("无效的隧道配置")
	}
	
	// 保存tunnel.json
	tunnelJSONPath := filepath.Join(a.config.FilePath, "tunnel.json")
	if err := os.WriteFile(tunnelJSONPath, []byte(a.config.ArgoAuth), 0644); err != nil {
		return err
	}
	
	// 生成tunnel.yml
	tunnelYAML := fmt.Sprintf(`
tunnel: %s
credentials-file: %s
ingress:
  - hostname: %s
    service: http://localhost:%d
  - service: http_status:404
`, tunnelID, tunnelJSONPath, a.config.ArgoDomain, a.config.ExternalPort)
	
	tunnelYAMLPath := filepath.Join(a.config.FilePath, "tunnel.yml")
	return os.WriteFile(tunnelYAMLPath, []byte(tunnelYAML), 0644)
}

func (a *App) generateXrayConfig() error {
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
						"id": a.config.UUID,
					}},
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
	
	configPath := filepath.Join(a.config.FilePath, "config.json")
	data, _ := json.MarshalIndent(config, "", "  ")
	return os.WriteFile(configPath, data, 0644)
}

// ==============================
// HTTP服务器启动
// ==============================
func (a *App) startHTTPServer() error {
	addr := fmt.Sprintf(":%d", a.config.Port)
	a.httpServer = &http.Server{
		Addr:         addr,
		Handler:      a.router,
		ReadTimeout:  time.Duration(a.config.RequestTimeout) * time.Second,
		WriteTimeout: time.Duration(a.config.RequestTimeout) * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	
	go func() {
		a.logger.Infof("HTTP服务器启动在端口 %d", a.config.Port)
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Fatalf("HTTP服务器启动失败: %v", err)
		}
	}()
	
	return nil
}

// ==============================
// 代理服务器启动
// ==============================
func (a *App) startProxyServer() error {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		var targetURL string
		
		if strings.Contains(path, "-argo") {
			targetURL = "http://localhost:3001"
		} else {
			targetURL = fmt.Sprintf("http://localhost:%d", a.config.Port)
		}
		
		target, _ := url.Parse(targetURL)
		proxy := httputil.NewSingleHostReverseProxy(target)
		
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Host = target.Host
		
		proxy.ServeHTTP(w, r)
	})
	
	addr := fmt.Sprintf(":%d", a.config.ExternalPort)
	a.proxyServer = &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	
	go func() {
		a.logger.Infof("代理服务器启动在端口 %d", a.config.ExternalPort)
		if err := a.proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Fatalf("代理服务器启动失败: %v", err)
		}
	}()
	
	return nil
}

// ==============================
// 主启动函数
// ==============================
func (a *App) Start() error {
	a.logger.Infof("🚀 启动 %s v%s...", a.config.AppName, a.config.AppVersion)
	a.logger.Infof("📁 数据目录: %s", a.config.FilePath)
	a.logger.Infof("🔑 UUID: %s", a.config.UUID)
	
	// 1. 清理旧文件
	a.cleanupOldFiles()
	
	// 2. 下载必要文件
	a.logger.Info("📥 正在下载必要文件...")
	if err := a.downloadFiles(); err != nil {
		return fmt.Errorf("下载文件失败: %v", err)
	}
	
	// 3. 启动所有服务
	a.logger.Info("🚀 正在启动服务...")
	
	// 启动哪吒监控
	if err := a.startNezha(); err != nil {
		a.logger.Errorf("启动哪吒监控失败: %v", err)
	}
	time.Sleep(2 * time.Second)
	
	// 启动Xray
	if err := a.startXray(); err != nil {
		a.logger.Errorf("启动Xray失败: %v", err)
	}
	time.Sleep(2 * time.Second)
	
	// 启动隧道
	if err := a.startTunnel(); err != nil {
		a.logger.Errorf("启动隧道失败: %v", err)
	}
	time.Sleep(5 * time.Second)
	
	// 4. 启动HTTP服务器
	a.logger.Info("🌐 正在启动HTTP服务器...")
	if err := a.startHTTPServer(); err != nil {
		return fmt.Errorf("启动HTTP服务器失败: %v", err)
	}
	
	// 5. 启动代理服务器
	a.logger.Info("🔄 正在启动代理服务器...")
	if err := a.startProxyServer(); err != nil {
		return fmt.Errorf("启动代理服务器失败: %v", err)
	}
	
	// 6. 启动监控
	go a.startMonitoring()
	
	a.logger.Info("✅ 应用启动完成!")
	a.logger.Info("==========================================")
	a.logger.Infof("📊 控制面板: http://localhost:%d", a.config.Port)
	a.logger.Infof("🔗 订阅地址: http://localhost:%d/%s", a.config.Port, a.config.SubPath)
	a.logger.Infof("📈 状态监控: http://localhost:%d/api/status", a.config.Port)
	a.logger.Info("==========================================")
	
	return nil
}

func (a *App) cleanupOldFiles() {
	// 清理临时文件，保留重要文件
	files, err := os.ReadDir(a.config.FilePath)
	if err != nil {
		return
	}
	
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		
		filename := file.Name()
		if filename == "sub.txt" || 
		   strings.HasSuffix(filename, ".json") ||
		   strings.HasSuffix(filename, ".yaml") ||
		   strings.HasSuffix(filename, ".yml") {
			continue
		}
		
		// 删除临时文件
		if strings.HasPrefix(filename, "tmp_") {
			filePath := filepath.Join(a.config.FilePath, filename)
			os.Remove(filePath)
		}
	}
}

func (a *App) startMonitoring() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// 更新内存使用情况
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			
			a.metrics.mu.Lock()
			a.metrics.MemoryUsage = memStats.Alloc
			a.metrics.mu.Unlock()
			
			// 检查隧道域名
			domain := a.extractTunnelDomain()
			if domain != "" && a.daemon.tunnelDomain != domain {
				a.daemon.SetTunnelInfo(a.daemon.tunnelType, domain)
				a.logger.Infof("隧道域名更新: %s", domain)
				
				// 更新订阅
				if err := a.updateSubscription(domain); err != nil {
					a.logger.Errorf("更新订阅失败: %v", err)
				}
			}
			
		case <-a.daemon.ctx.Done():
			return
		}
	}
}

func (a *App) extractTunnelDomain() string {
	logPath := filepath.Join(a.config.FilePath, "cloudflared.log")
	
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return ""
	}
	
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

func (a *App) updateSubscription(domain string) error {
	subscription := a.generateSubscription(domain)
	encoded := base64.StdEncoding.EncodeToString([]byte(subscription))
	
	subPath := filepath.Join(a.config.FilePath, "sub.txt")
	return os.WriteFile(subPath, []byte(encoded), 0644)
}

// ==============================
// 优雅关闭
// ==============================
func (a *App) Shutdown() {
	a.logger.Info("正在关闭应用...")
	
	// 创建关闭超时
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// 关闭HTTP服务器
	if a.httpServer != nil {
		if err := a.httpServer.Shutdown(ctx); err != nil {
			a.logger.Errorf("HTTP服务器关闭失败: %v", err)
		}
	}
	
	// 关闭代理服务器
	if a.proxyServer != nil {
		if err := a.proxyServer.Shutdown(ctx); err != nil {
			a.logger.Errorf("代理服务器关闭失败: %v", err)
		}
	}
	
	// 清理守护进程
	a.daemon.Cleanup()
	
	a.logger.Info("应用已关闭")
}

// ==============================
// 主函数
// ==============================
func main() {
	// 创建应用
	app, err := NewApp()
	if err != nil {
		log.Fatalf("创建应用失败: %v", err)
	}
	
	// 捕获中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	
	// 启动应用
	go func() {
		if err := app.Start(); err != nil {
			log.Fatalf("启动应用失败: %v", err)
		}
	}()
	
	// 等待中断信号
	sig := <-sigChan
	app.logger.Infof("收到信号: %v，正在关闭...", sig)
	
	// 优雅关闭
	app.Shutdown()
	
	app.logger.Info("应用已停止")
}
