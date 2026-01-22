package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Config 配置结构体
type Config struct {
	UploadURL    string
	ProjectURL   string
	AutoAccess   bool
	FilePath     string
	SubPath      string
	Port         string
	ExternalPort string
	UUID         string  // 如果环境变量为空，将自动生成
	NezhaServer  string
	NezhaPort    string
	NezhaKey     string
	ArgoDomain   string
	ArgoAuth     string
	CFIP         string
	CFPort       string
	Name         string
	MonitorKey   string // 监控脚本密钥
	MonitorServer string // 监控服务器标识
	MonitorURL   string // 监控上报地址
}

// 全局变量
var (
	config       Config
	files        = make(map[string]string)
	mu           sync.RWMutex
	subscription string
	proxy        *httputil.ReverseProxy
	monitorProcess *os.Process
)

func main() {
	// 初始化配置
	initConfig()
	
	// 创建目录
	if err := os.MkdirAll(config.FilePath, 0755); err != nil {
		log.Printf("创建目录失败: %v", err)
	} else {
		log.Printf("目录 %s 已创建或已存在", config.FilePath)
	}
	
	// 生成随机文件名
	generateFilenames()
	
	// 清理历史文件和节点
	cleanup()
	
	// 生成配置文件
	generateXrayConfig()
	
	// 初始化代理
	initProxy()
	
	// 启动HTTP服务器
	go startHTTPServer()
	
	// 启动监控脚本
	go startMonitorScript()
	
	// 主流程
	go startMainProcess()
	
	// 设置信号处理，优雅关闭
	setupSignalHandler()
	
	// 保持程序运行
	select {}
}

func initConfig() {
	// 从环境变量获取UUID，如果为空则生成
	uuidFromEnv := getEnv("UUID", ``)
	if uuidFromEnv == `` {
		uuidFromEnv = generateUUID()
		log.Printf("环境变量UUID为空，已自动生成UUID: %s", uuidFromEnv)
	} else {
		log.Printf("使用环境变量中的UUID: %s", uuidFromEnv)
	}
	
	config = Config{
		UploadURL:    getEnv("UPLOAD_URL", ``),
		ProjectURL:   getEnv("PROJECT_URL", ``),
		AutoAccess:   getEnv("AUTO_ACCESS", `false`) == "true",
		FilePath:     getEnv("FILE_PATH", `./tmp`),
		SubPath:      getEnv("SUB_PATH", `sub`),
		Port:         getEnv("SERVER_PORT", getEnv("PORT", `3000`)),
		ExternalPort: getEnv("EXTERNAL_PORT", `7860`),
		UUID:         uuidFromEnv,
		NezhaServer:  getEnv("NEZHA_SERVER", ``),
		NezhaPort:    getEnv("NEZHA_PORT", ``),
		NezhaKey:     getEnv("NEZHA_KEY", ``),
		ArgoDomain:   getEnv("ARGO_DOMAIN", ``),
		ArgoAuth:     getEnv("ARGO_AUTH", ``),
		CFIP:         getEnv("CFIP", `cdns.doon.eu.org`),
		CFPort:       getEnv("CFPORT", `443`),
		Name:         getEnv("NAME", ``),
		MonitorKey:   getEnv("MONITOR_KEY", ``),
		MonitorServer: getEnv("MONITOR_SERVER", ``),
		MonitorURL:   getEnv("MONITOR_URL", ``),
	}
	
	log.Println("配置初始化完成")
	log.Printf("最终使用的UUID: %s", config.UUID)
	
	// 输出监控配置信息
	if config.MonitorKey != `` && config.MonitorServer != `` && config.MonitorURL != `` {
		log.Println("监控脚本已配置，将自动运行")
		log.Printf("监控服务器: %s", config.MonitorServer)
		log.Printf("监控URL: %s", config.MonitorURL)
	}
}

// 生成UUID v4
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		log.Printf("生成UUID时出错: %v", err)
		// 如果随机生成失败，使用基于时间的UUID
		return generateTimeBasedUUID()
	}
	
	// 设置版本号 (4)
	b[6] = (b[6] & 0x0f) | 0x40
	// 设置变体 (10)
	b[8] = (b[8] & 0x3f) | 0x80
	
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// 基于时间的UUID生成器（备用）
func generateTimeBasedUUID() string {
	now := time.Now().UnixNano()
	randomPart := make([]byte, 8)
	rand.Read(randomPart)
	
	return fmt.Sprintf("%016x-%04x-%04x-%04x-%012x",
		now,
		(now>>48)&0xffff,
		(now>>32)&0xffff,
		(now>>16)&0xffff,
		randomPart)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != `` {
		return value
	}
	return defaultValue
}

func generateFilenames() {
	// 生成6位随机小写字母
	randomName := func() string {
		const letters = "abcdefghijklmnopqrstuvwxyz"
		b := make([]byte, 6)
		rand.Read(b)
		for i := range b {
			b[i] = letters[int(b[i])%len(letters)]
		}
		return string(b)
	}
	
	files["npm"] = filepath.Join(config.FilePath, randomName())
	files["web"] = filepath.Join(config.FilePath, randomName())
	files["bot"] = filepath.Join(config.FilePath, randomName())
	files["php"] = filepath.Join(config.FilePath, randomName())
	files["monitor"] = filepath.Join(config.FilePath, "cf-vps-monitor.sh")
	files["sub"] = filepath.Join(config.FilePath, "sub.txt")
	files["list"] = filepath.Join(config.FilePath, "list.txt")
	files["bootLog"] = filepath.Join(config.FilePath, "boot.log")
	files["config"] = filepath.Join(config.FilePath, "config.json")
	files["nezhaConfig"] = filepath.Join(config.FilePath, "config.yaml")
	files["tunnelJson"] = filepath.Join(config.FilePath, "tunnel.json")
	files["tunnelYaml"] = filepath.Join(config.FilePath, "tunnel.yml")
	
	log.Println("文件名生成完成")
}

func cleanup() {
	// 清理旧文件
	if err := os.RemoveAll(config.FilePath); err != nil {
		log.Printf("清理目录失败: %v", err)
	}
	
	// 重新创建目录
	os.MkdirAll(config.FilePath, 0755)
	
	// 删除历史节点
	deleteNodes()
}

func deleteNodes() {
	if config.UploadURL == `` {
		return
	}
	
	// 读取订阅文件
	data, err := os.ReadFile(files["sub"])
	if err != nil {
		return
	}
	
	// 解码base64
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return
	}
	
	// 解析节点
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
	
	// 发送删除请求
	jsonData, _ := json.Marshal(map[string][]string{"nodes": nodes})
	req, err := http.NewRequest("POST", config.UploadURL+"/api/delete-nodes", 
		bytes.NewBuffer(jsonData))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 10 * time.Second}
	_, err = client.Do(req)
	if err != nil {
		log.Printf("删除节点失败: %v", err)
	}
}

func generateXrayConfig() {
	xrayConfig := map[string]interface{}{
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
				"port": 3001,
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{
						{
							"id":   config.UUID,
							"flow": "xtls-rprx-vision",
						},
					},
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
					"clients": []map[string]interface{}{
						{"id": config.UUID},
					},
					"decryption": "none",
				},
				"streamSettings": map[string]interface{}{
					"network":  "tcp",
					"security": "none",
				},
			},
			{
				"port":     3003,
				"listen":   "127.0.0.1",
				"protocol": "vless",
				"settings": map[string]interface{}{
					"clients": []map[string]interface{}{
						{"id": config.UUID, "level": 0},
					},
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
					"clients": []map[string]interface{}{
						{"id": config.UUID, "alterId": 0},
					},
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
					"clients": []map[string]interface{}{
						{"password": config.UUID},
					},
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
				"settings": map[string]interface{}{},
			},
		},
		"routing": map[string]interface{}{
			"domainStrategy": "IPIfNonMatch",
			"rules":          []interface{}{},
		},
	}
	
	// 写入配置文件
	data, err := json.MarshalIndent(xrayConfig, "", "  ")
	if err != nil {
		log.Printf("生成配置文件失败: %v", err)
		return
	}
	
	if err := os.WriteFile(files["config"], data, 0644); err != nil {
		log.Printf("写入配置文件失败: %v", err)
		return
	}
	
	log.Println("Xray配置文件生成完成")
}

func initProxy() {
	proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			path := req.URL.Path
			
			// 判断目标地址
			if strings.HasPrefix(path, "/vless-argo") || 
			   strings.HasPrefix(path, "/vmess-argo") || 
			   strings.HasPrefix(path, "/trojan-argo") ||
			   path == "/vless" || 
			   path == "/vmess" || 
			   path == "/trojan" {
				req.URL.Scheme = "http"
				req.URL.Host = "localhost:3001"
			} else {
				req.URL.Scheme = "http"
				req.URL.Host = "localhost:" + config.Port
			}
			
			req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
			req.Host = req.URL.Host
		},
	}
}

func startHTTPServer() {
	// 代理服务器
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 特殊路径处理
		path := r.URL.Path
		
		// 如果是订阅路径
		if path == "/"+config.SubPath || path == "/"+config.SubPath+"/" {
			mu.RLock()
			encoded := base64.StdEncoding.EncodeToString([]byte(subscription))
			mu.RUnlock()
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte(encoded))
			return
		}
		
		// 根路径
		if path == "/" {
			// 检查index.html文件
			if _, err := os.Stat("index.html"); err == nil {
				http.ServeFile(w, r, "index.html")
			} else if _, err := os.Stat("/app/index.html"); err == nil {
				http.ServeFile(w, r, "/app/index.html")
			} else {
				w.Write([]byte("Hello world!"))
			}
			return
		}
		
		// 代理其他请求
		proxy.ServeHTTP(w, r)
	})
	
	// 启动外部端口代理
	go func() {
		log.Printf("外部代理服务启动在端口: %s", config.ExternalPort)
		if err := http.ListenAndServe(":"+config.ExternalPort, nil); err != nil {
			log.Printf("外部代理服务启动失败: %v", err)
		}
	}()
	
	// 启动内部HTTP服务
	log.Printf("内部HTTP服务启动在端口: %s", config.Port)
	if err := http.ListenAndServe(":"+config.Port, nil); err != nil {
		log.Printf("内部HTTP服务启动失败: %v", err)
	}
}

// 启动监控脚本
func startMonitorScript() {
	// 检查监控配置是否完整
	if config.MonitorKey == `` || config.MonitorServer == `` || config.MonitorURL == `` {
		log.Println("监控环境变量不完整，跳过监控脚本启动")
		return
	}
	
	// 等待一段时间，确保其他服务已启动
	time.Sleep(10 * time.Second)
	
	log.Println("开始下载并运行监控脚本...")
	
	// 下载监控脚本
	if err := downloadMonitorScript(); err != nil {
		log.Printf("下载监控脚本失败: %v", err)
		return
	}
	
	// 设置执行权限
	if err := os.Chmod(files["monitor"], 0755); err != nil {
		log.Printf("设置监控脚本执行权限失败: %v", err)
		return
	}
	
	// 运行监控脚本
	go runMonitorScript()
}

// 下载监控脚本
func downloadMonitorScript() error {
	monitorURL := "https://raw.githubusercontent.com/kadidalax/cf-vps-monitor/main/cf-vps-monitor.sh"
	
	log.Printf("从 %s 下载监控脚本", monitorURL)
	
	// 创建文件
	out, err := os.Create(files["monitor"])
	if err != nil {
		return err
	}
	defer out.Close()
	
	// 下载文件
	resp, err := http.Get(monitorURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载监控脚本失败: %s", resp.Status)
	}
	
	// 写入文件
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}
	
	log.Println("监控脚本下载完成")
	return nil
}

// 运行监控脚本
func runMonitorScript() {
	// 构建命令参数
	args := []string{
		"-i",                    // 安装模式
		"-k", config.MonitorKey, // 密钥
		"-s", config.MonitorServer, // 服务器标识
		"-u", config.MonitorURL, // 上报地址
	}
	
	log.Printf("运行监控脚本: %s %s", files["monitor"], strings.Join(args, " "))
	
	// 执行命令
	cmd := exec.Command(files["monitor"], args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		log.Printf("运行监控脚本失败: %v", err)
		// 尝试直接执行命令
		runDirectMonitor()
		return
	}
	
	// 保存进程引用
	monitorProcess = cmd.Process
	log.Println("监控脚本启动成功")
	
	// 如果进程退出，尝试重启
	go func() {
		cmd.Wait()
		log.Println("监控脚本已退出，将在30秒后重启...")
		time.Sleep(30 * time.Second)
		runMonitorScript()
	}()
}

// 直接运行监控命令（备用方法）
func runDirectMonitor() {
	log.Println("尝试直接运行监控命令...")
	
	// 构建wget命令
	wgetCmd := fmt.Sprintf("wget https://raw.githubusercontent.com/kadidalax/cf-vps-monitor/main/cf-vps-monitor.sh -O %s && chmod +x %s && %s -i -k %s -s %s -u %s",
		files["monitor"], files["monitor"], files["monitor"],
		config.MonitorKey, config.MonitorServer, config.MonitorURL)
	
	cmd := exec.Command("sh", "-c", wgetCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		log.Printf("直接运行监控命令失败: %v", err)
	} else {
		log.Println("监控命令执行成功")
	}
}

func startMainProcess() {
	// 延时启动，确保服务器已启动
	time.Sleep(2 * time.Second)
	
	// 生成Argo隧道配置
	argoType()
	
	// 下载文件
	downloadFiles()
	
	// 运行哪吒监控
	runNezha()
	
	// 运行Xray
	runXray()
	
	// 运行Cloudflared
	runCloudflared()
	
	// 等待隧道启动
	time.Sleep(5 * time.Second)
	
	// 提取域名并生成订阅
	extractDomains()
	
	// 上传节点
	uploadNodes()
	
	// 自动访问任务
	addVisitTask()
	
	// 清理文件（90秒后）
	go func() {
		time.Sleep(90 * time.Second)
		cleanFiles()
	}()
}

func argoType() {
	if config.ArgoAuth == `` || config.ArgoDomain == `` {
		log.Println("ARGO_DOMAIN 或 ARGO_AUTH 为空，使用快速隧道")
		return
	}
	
	// 检查是否为TunnelSecret格式
	if strings.Contains(config.ArgoAuth, "TunnelSecret") {
		var tunnelConfig map[string]interface{}
		if err := json.Unmarshal([]byte(config.ArgoAuth), &tunnelConfig); err != nil {
			log.Printf("解析隧道配置失败: %v", err)
			return
		}
		
		// 写入tunnel.json
		if err := os.WriteFile(files["tunnelJson"], []byte(config.ArgoAuth), 0644); err != nil {
			log.Printf("写入tunnel.json失败: %v", err)
			return
		}
		
		// 生成tunnel.yml
		tunnelID, _ := tunnelConfig["TunnelID"].(string)
		yamlContent := fmt.Sprintf(`tunnel: %s
credentials-file: %s
protocol: http2

ingress:
  - hostname: %s
    service: http://localhost:%s
    originRequest:
      noTLSVerify: true
  - service: http_status:404
`, tunnelID, files["tunnelJson"], config.ArgoDomain, config.ExternalPort)
		
		if err := os.WriteFile(files["tunnelYaml"], []byte(yamlContent), 0644); err != nil {
			log.Printf("写入tunnel.yml失败: %v", err)
			return
		}
		
		log.Println("隧道YAML配置生成成功")
	} else {
		log.Println("ARGO_AUTH 不是TunnelSecret格式，使用token连接隧道")
	}
}

func downloadFiles() {
	// 获取系统架构
	arch := getArchitecture()
	
	// 确定下载URL
	var baseURL string
	if arch == "arm" {
		baseURL = "https://arm64.ssss.nyc.mn/"
	} else {
		baseURL = "https://amd64.ssss.nyc.mn/"
	}
	
	// 需要下载的文件
	fileList := []struct {
		name     string
		filePath string
		url      string
	}{
		{"web", files["web"], baseURL + "web"},
		{"bot", files["bot"], baseURL + "bot"},
	}
	
	// 如果需要哪吒监控
	if config.NezhaServer != `` && config.NezhaKey != `` {
		if config.NezhaPort != `` {
			fileList = append([]struct {
				name     string
				filePath string
				url      string
			}{
				{"agent", files["npm"], baseURL + "agent"},
			}, fileList...)
		} else {
			fileList = append([]struct {
				name     string
				filePath string
				url      string
			}{
				{"php", files["php"], baseURL + "v1"},
			}, fileList...)
		}
	}
	
	// 下载文件
	var wg sync.WaitGroup
	for _, file := range fileList {
		wg.Add(1)
		go func(name, filePath, url string) {
			defer wg.Done()
			if err := downloadFile(filePath, url); err != nil {
				log.Printf("下载 %s 失败: %v", name, err)
			} else {
				log.Printf("下载 %s 成功", name)
				// 设置执行权限
				os.Chmod(filePath, 0755)
			}
		}(file.name, file.filePath, file.url)
	}
	wg.Wait()
	
	log.Println("所有文件下载完成")
}

func getArchitecture() string {
	arch := runtime.GOARCH
	if arch == "arm" || arch == "arm64" || arch == "aarch64" {
		return "arm"
	}
	return "amd"
}

func downloadFile(filepath, url string) error {
	// 创建文件
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()
	
	// 下载文件
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: %s", resp.Status)
	}
	
	// 写入文件
	_, err = io.Copy(out, resp.Body)
	return err
}

func runNezha() {
	if config.NezhaServer == `` || config.NezhaKey == `` {
		log.Println("哪吒监控变量为空，跳过运行")
		return
	}
	
	if config.NezhaPort == `` {
		// v1版本
		port := "443"
		if idx := strings.LastIndex(config.NezhaServer, ":"); idx != -1 {
			port = config.NezhaServer[idx+1:]
		}
		
		// 检查是否为TLS端口
		tlsPorts := map[string]bool{
			"443":  true,
			"8443": true,
			"2096": true,
			"2087": true,
			"2083": true,
			"2053": true,
		}
		
		nezhatls := "false"
		if tlsPorts[port] {
			nezhatls = "true"
		}
		
		// 生成配置文件
		yamlContent := fmt.Sprintf(`client_secret: %s
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
uuid: %s`, config.NezhaKey, config.NezhaServer, nezhatls, config.UUID)
		
		if err := os.WriteFile(files["nezhaConfig"], []byte(yamlContent), 0644); err != nil {
			log.Printf("生成哪吒配置失败: %v", err)
			return
		}
		
		// 运行哪吒
		cmd := exec.Command(files["php"], "-c", files["nezhaConfig"])
		if err := cmd.Start(); err != nil {
			log.Printf("运行哪吒失败: %v", err)
			return
		}
		
		go cmd.Wait()
		log.Printf("%s 运行中", filepath.Base(files["php"]))
		
	} else {
		// v0版本
		var args []string
		args = append(args, "-s", config.NezhaServer+":"+config.NezhaPort)
		args = append(args, "-p", config.NezhaKey)
		
		// 检查是否为TLS端口
		tlsPorts := map[string]bool{
			"443":  true,
			"8443": true,
			"2096": true,
			"2087": true,
			"2083": true,
			"2053": true,
		}
		
		if tlsPorts[config.NezhaPort] {
			args = append(args, "--tls")
		}
		
		args = append(args, "--disable-auto-update", "--report-delay", "4", "--skip-conn", "--skip-procs")
		
		cmd := exec.Command(files["npm"], args...)
		if err := cmd.Start(); err != nil {
			log.Printf("运行哪吒失败: %v", err)
			return
		}
		
		go cmd.Wait()
		log.Printf("%s 运行中", filepath.Base(files["npm"]))
	}
	
	time.Sleep(1 * time.Second)
}

func runXray() {
	cmd := exec.Command(files["web"], "-c", files["config"])
	if err := cmd.Start(); err != nil {
		log.Printf("运行Xray失败: %v", err)
		return
	}
	
	go cmd.Wait()
	log.Printf("%s 运行中", filepath.Base(files["web"]))
	time.Sleep(1 * time.Second)
}

func runCloudflared() {
	if _, err := os.Stat(files["bot"]); os.IsNotExist(err) {
		log.Println("cloudflared文件不存在")
		return
	}
	
	var args []string
	args = append(args, "tunnel", "--edge-ip-version", "auto", "--no-autoupdate", "--protocol", "http2")
	
	if config.ArgoAuth != `` && config.ArgoDomain != `` {
		if strings.Contains(config.ArgoAuth, "TunnelSecret") {
			args = append(args, "--config", files["tunnelYaml"], "run")
		} else if len(config.ArgoAuth) >= 120 && len(config.ArgoAuth) <= 250 {
			args = append(args, "run", "--token", config.ArgoAuth)
		} else {
			args = append(args, "--logfile", files["bootLog"], "--loglevel", "info", 
				"--url", "http://localhost:"+config.ExternalPort)
		}
	} else {
		args = append(args, "--logfile", files["bootLog"], "--loglevel", "info", 
			"--url", "http://localhost:"+config.ExternalPort)
	}
	
	cmd := exec.Command(files["bot"], args...)
	if err := cmd.Start(); err != nil {
		log.Printf("运行cloudflared失败: %v", err)
		return
	}
	
	go cmd.Wait()
	log.Printf("%s 运行中", filepath.Base(files["bot"]))
	
	// 等待隧道启动
	time.Sleep(5 * time.Second)
	
	// 检查隧道是否运行
	if config.ArgoAuth != `` && strings.Contains(config.ArgoAuth, "TunnelSecret") {
		if cmd.Process == nil {
			log.Println("隧道启动失败")
		} else {
			log.Println("隧道运行成功")
		}
	}
}

func extractDomains() {
	// 如果配置了固定域名
	if config.ArgoAuth != `` && config.ArgoDomain != `` {
		argoDomain := config.ArgoDomain
		log.Printf("使用固定域名: %s", argoDomain)
		generateLinks(argoDomain)
		return
	}
	
	// 从日志文件读取临时域名
	data, err := os.ReadFile(files["bootLog"])
	if err != nil {
		log.Printf("读取日志文件失败: %v", err)
		// 重启cloudflared获取域名
		restartCloudflared()
		return
	}
	
	// 查找域名
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, "trycloudflare.com") {
			// 提取域名
			start := strings.Index(line, "https://")
			if start == -1 {
				start = strings.Index(line, "http://")
			}
			if start != -1 {
				end := strings.Index(line[start:], " ")
				if end == -1 {
					end = len(line) - start
				}
				url := line[start : start+end]
				argoDomain := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
				argoDomain = strings.TrimSuffix(argoDomain, "/")
				log.Printf("找到临时域名: %s", argoDomain)
				generateLinks(argoDomain)
				return
			}
		}
	}
	
	log.Println("未找到域名，尝试重启cloudflared")
	restartCloudflared()
}

func restartCloudflared() {
	// 停止现有进程
	exec.Command("pkill", "-f", filepath.Base(files["bot"])).Run()
	
	// 删除日志文件
	os.Remove(files["bootLog"])
	
	time.Sleep(3 * time.Second)
	
	// 重新启动
	args := []string{
		"tunnel", "--edge-ip-version", "auto", "--no-autoupdate", "--protocol", "http2",
		"--logfile", files["bootLog"], "--loglevel", "info",
		"--url", "http://localhost:" + config.ExternalPort,
	}
	
	cmd := exec.Command(files["bot"], args...)
	if err := cmd.Start(); err != nil {
		log.Printf("重启cloudflared失败: %v", err)
		return
	}
	
	go cmd.Wait()
	
	time.Sleep(3 * time.Second)
	extractDomains()
}

func generateLinks(domain string) {
	// 获取ISP信息
	isp := getISP()
	nodeName := config.Name
	if nodeName != `` {
		nodeName = nodeName + "-" + isp
	} else {
		nodeName = isp
	}
	
	// 生成VMESS配置
	vmessConfig := map[string]interface{}{
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
		"fp":   "firefox",
	}
	
	vmessJSON, _ := json.Marshal(vmessConfig)
	vmessBase64 := base64.StdEncoding.EncodeToString(vmessJSON)
	
	// 生成订阅内容
	subTxt := fmt.Sprintf(`
vless://%s@%s:%s?encryption=none&security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Fvless-argo%%3Fed%%3D2560#%s

vmess://%s

trojan://%s@%s:%s?security=tls&sni=%s&fp=firefox&type=ws&host=%s&path=%%2Ftrojan-argo%%3Fed%%3D2560#%s
`, config.UUID, config.CFIP, config.CFPort, domain, domain, nodeName,
		vmessBase64,
		config.UUID, config.CFIP, config.CFPort, domain, domain, nodeName)
	
	// 更新订阅缓存
	mu.Lock()
	subscription = subTxt
	mu.Unlock()
	
	// 保存到文件
	encoded := base64.StdEncoding.EncodeToString([]byte(subTxt))
	if err := os.WriteFile(files["sub"], []byte(encoded), 0644); err != nil {
		log.Printf("保存订阅文件失败: %v", err)
	} else {
		log.Printf("订阅文件已保存: %s", files["sub"])
	}
	
	log.Printf("订阅内容:\n%s", encoded)
}

func getISP() string {
	// 尝试获取IP信息
	client := &http.Client{Timeout: 3 * time.Second}
	
	// 第一个API
	resp, err := client.Get("https://ipapi.co/json/")
	if err == nil {
		defer resp.Body.Close()
		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if country, ok := data["country_code"].(string); ok {
				if org, ok := data["org"].(string); ok {
					return strings.ReplaceAll(country+"_"+org, " ", "_")
				}
			}
		}
	}
	
	// 备用API
	resp, err = client.Get("http://ip-api.com/json/")
	if err == nil {
		defer resp.Body.Close()
		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if status, ok := data["status"].(string); ok && status == "success" {
				if country, ok := data["countryCode"].(string); ok {
					if org, ok := data["org"].(string); ok {
						return strings.ReplaceAll(country+"_"+org, " ", "_")
					}
				}
			}
		}
	}
	
	return "Unknown"
}

func uploadNodes() {
	if config.UploadURL == `` {
		return
	}
	
	if config.ProjectURL != `` {
		// 上传订阅
		subscriptionUrl := config.ProjectURL + "/" + config.SubPath
		jsonData := map[string][]string{
			"subscription": {subscriptionUrl},
		}
		
		data, _ := json.Marshal(jsonData)
		req, err := http.NewRequest("POST", config.UploadURL+"/api/add-subscriptions", 
			bytes.NewBuffer(data))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		
		if err == nil && resp.StatusCode == 200 {
			log.Println("订阅上传成功")
		} else {
			log.Printf("订阅上传失败: %v", err)
		}
	} else {
		// 上传节点
		if _, err := os.Stat(files["list"]); os.IsNotExist(err) {
			return
		}
		
		data, err := os.ReadFile(files["list"])
		if err != nil {
			return
		}
		
		lines := strings.Split(string(data), "\n")
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
		
		jsonData, _ := json.Marshal(map[string][]string{"nodes": nodes})
		req, err := http.NewRequest("POST", config.UploadURL+"/api/add-nodes", 
			bytes.NewBuffer(jsonData))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		
		if err == nil && resp.StatusCode == 200 {
			log.Println("节点上传成功")
		}
	}
}

func addVisitTask() {
	if !config.AutoAccess || config.ProjectURL == `` {
		log.Println("跳过自动访问任务")
		return
	}
	
	jsonData := map[string]string{"url": config.ProjectURL}
	data, _ := json.Marshal(jsonData)
	
	req, err := http.NewRequest("POST", "https://oooo.serv00.net/add-url", 
		bytes.NewBuffer(data))
	if err != nil {
		log.Printf("创建请求失败: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	
	if err == nil && resp.StatusCode == 200 {
		log.Println("自动访问任务添加成功")
	} else {
		log.Printf("添加自动访问任务失败: %v", err)
	}
}

func cleanFiles() {
	// 要删除的文件列表
	filesToDelete := []string{
		files["bootLog"],
		files["config"],
		files["web"],
		files["bot"],
		files["monitor"],
	}
	
	if config.NezhaPort != `` {
		filesToDelete = append(filesToDelete, files["npm"])
	} else if config.NezhaServer != `` && config.NezhaKey != `` {
		filesToDelete = append(filesToDelete, files["php"])
	}
	
	// 删除文件
	for _, file := range filesToDelete {
		if err := os.Remove(file); err != nil {
			log.Printf("删除文件失败 %s: %v", file, err)
		}
	}
	
	log.Println("应用正在运行")
	log.Println("感谢使用此脚本，享受吧！")
}

// 设置信号处理，优雅关闭
func setupSignalHandler() {
	// 在Go中，我们可以监听中断信号
	c := make(chan os.Signal, 1)
	// signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	
	go func() {
		<-c
		log.Println("收到关闭信号，正在清理...")
		
		// 停止监控进程
		if monitorProcess != nil {
			log.Println("停止监控脚本...")
			monitorProcess.Kill()
		}
		
		log.Println("程序退出")
		os.Exit(0)
	}()
}
