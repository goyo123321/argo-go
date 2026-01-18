package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// 环境变量配置
type Config struct {
	UploadURL     string
	ProjectURL    string
	AutoAccess    bool
	FilePath      string
	SubPath       string
	Port          string
	UUID          string
	NezhaServer   string
	NezhaPort     string
	NezhaKey      string
	ArgoDomain    string
	ArgoAuth      string
	ExternalPort  int
	CFIP          string
	CFPort        int
	Name          string
}

func loadConfig() *Config {
	return &Config{
		UploadURL:     getEnv("UPLOAD_URL", ""),  // 节点上传地址,需要先部署Merge-sub项目，例如：https://merge.serv00.net
		ProjectURL:    getEnv("PROJECT_URL", ""), // 项目地址,需上传订阅或保活要填这个地址,例如：https://google.com
		AutoAccess:    getEnvAsBool("AUTO_ACCESS", false), // 是否开启自动东保活,false关闭,true开启
		FilePath:      getEnv("FILE_PATH", "./tmp"),       // 运行路径，sub.txt保存路径
		SubPath:       getEnv("SUB_PATH", "sub"),          // 订阅路径,例如：https://google.com/sub
		Port:          getEnv("SERVER_PORT", getEnv("PORT", "3000")), // http服务端口
		UUID:          getEnv("UUID", "4b3e2bfe-bde1-5def-d035-0cb572bbd046"), // 哪吒v1在不同的平台部署需要修改
		NezhaServer:   getEnv("NEZHA_SERVER", "gwwjllhldpjy.us-west-1.clawcloudrun.com:443"), // 哪吒v1填写形式：nz.abc.com:8008  哪吒v0填写形式：nz.abc.com
		NezhaPort:     getEnv("NEZHA_PORT", ""),   // 哪吒v1留空此项，v0的agent端口
		NezhaKey:      getEnv("NEZHA_KEY", "rRA5ZrgOmsosl7EiyIuJBhnGwcAqWDUr"),    // 哪吒v1的NZ_CLIENT_SECRET或v0的agent密钥
		ArgoDomain:    getEnv("ARGO_DOMAIN", "hug3.bgxzg.indevs.in"),  // 固定隧道域名，留空即启用临时隧道
		ArgoAuth:      getEnv("ARGO_AUTH", "eyJhIjoiMzZhYzM1MmM5YmY2N2M1MzE0ZGJmYmE3MzFmMmIzMTkiLCJ0IjoiMWFhZmZiYmMtMTViZi00M2U0LTk1ZTUtZDdiMGJlODYxOTViIiwicyI6Ik9UUXdaV1EyTTJNdFpqUmhNUzAwWW1Sa0xUaG1ZVEl0WkdVeE5tTmpOR1F5WldaaiJ9"),    // 固定隧道json或token，留空即启用临时隧道
		ExternalPort:  getEnvAsInt("EXTERNAL_PORT", 7860), // 外部端口，隧道转发到的端口
		CFIP:          getEnv("CFIP", "www.visa.com.tw"), // 优选域名或优选ip
		CFPort:        getEnvAsInt("CFPORT", 443),        // 优选域名或优选ip对应的端口
		Name:          getEnv("NAME", "Vls"),             // 节点名称
	}
}

// 创建运行目录
func getEnvAsBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		return strings.ToLower(value) == "true"
	}
	return defaultValue
}

// 删除历史节点
func deleteNodes(cfg *Config) error {
	if cfg.UploadURL == "" {
		return nil
	}

	subPath := filepath.Join(cfg.FilePath, "sub.txt")
	if _, err := os.Stat(subPath); os.IsNotExist(err) {
		return nil
	}

	content, err := os.ReadFile(subPath)
	if err != nil {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(string(content))
	if err != nil {
		return nil
	}

	nodes := []string{}
	for _, line := range strings.Split(string(decoded), "\n") {
		if matched, _ := regexp.MatchString(`(vless|vmess|trojan|hysteria2|tuic)://`, line); matched {
			nodes = append(nodes, strings.TrimSpace(line))
		}
	}

	if len(nodes) == 0 {
		return nil
	}

	jsonData := map[string]interface{}{
		"nodes": nodes,
	}
	
	jsonBytes, _ := json.Marshal(jsonData)
	resp, err := http.Post(cfg.UploadURL+"/api/delete-nodes", 
		"application/json", 
		bytes.NewBuffer(jsonBytes))
	
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	
	return nil
}

// 上传节点或订阅
func uploadNodes(cfg *Config) {
	if cfg.UploadURL == "" && cfg.ProjectURL == "" {
		return
	}

	if cfg.UploadURL != "" && cfg.ProjectURL != "" {
		// 上传订阅
		subscriptionUrl := fmt.Sprintf("%s/%s", cfg.ProjectURL, cfg.SubPath)
		jsonData := map[string]interface{}{
			"subscription": []string{subscriptionUrl},
		}
		
		jsonBytes, _ := json.Marshal(jsonData)
		resp, err := http.Post(cfg.UploadURL+"/api/add-subscriptions", 
			"application/json", 
			bytes.NewBuffer(jsonBytes))
		
		if err == nil && resp.StatusCode == 200 {
			log.Println("Subscription uploaded successfully")
		}
		if resp != nil {
			resp.Body.Close()
		}
	} else if cfg.UploadURL != "" {
		// 上传节点
		subPath := filepath.Join(cfg.FilePath, "sub.txt")
		if _, err := os.Stat(subPath); os.IsNotExist(err) {
			return
		}
	
		content, err := os.ReadFile(subPath)
		if err != nil {
			return
		}
				
		decoded, err := base64.StdEncoding.DecodeString(string(content))
		if err != nil {
			return
		}
	
		nodes := []string{}
		for _, line := range strings.Split(string(decoded), "\n") {  // Changed from content to decoded
			if matched, _ := regexp.MatchString(`(vless|vmess|trojan|hysteria2|tuic)://`, line); matched {
				nodes = append(nodes, strings.TrimSpace(line))
			}
		}

		if len(nodes) == 0 {
			return
		}

		jsonData := map[string]interface{}{
			"nodes": nodes,
		}
		
		jsonBytes, _ := json.Marshal(jsonData)
		resp, err := http.Post(cfg.UploadURL+"/api/add-nodes", 
			"application/json", 
			bytes.NewBuffer(jsonBytes))
		
		if err == nil && resp.StatusCode == 200 {
			log.Println("Nodes uploaded successfully")
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
}

// 添加自动访问任务
func addVisitTask(cfg *Config) {
	if !cfg.AutoAccess || cfg.ProjectURL == "" {
		log.Println("Skipping adding automatic access task")
		return
	}

	jsonData := map[string]string{
		"url": cfg.ProjectURL,
	}
	
	jsonBytes, _ := json.Marshal(jsonData)
	resp, err := http.Post("https://gifted-steel-cheek.glitch.me/add-url", 
		"application/json", 
		bytes.NewBuffer(jsonBytes))
	
	if err != nil {
		log.Printf("添加URL失败: %v", err)
		return
	}
	defer resp.Body.Close()

	log.Println("automatic access task added successfully")
}

// XRay配置结构
type XRayConfig struct {
	Log       LogConfig       `json:"log"`
	Inbounds  []Inbound      `json:"inbounds"`
	DNS       DNSConfig      `json:"dns"`
	Outbounds []Outbound     `json:"outbounds"`
	Routing   RoutingConfig  `json:"routing"`
}

type LogConfig struct {
	Access   string `json:"access"`
	Error    string `json:"error"`
	Loglevel string `json:"loglevel"`
}

type Inbound struct {
	Port           int                    `json:"port"`
	Protocol       string                 `json:"protocol"`
	Settings       map[string]interface{} `json:"settings"`
	StreamSettings map[string]interface{} `json:"streamSettings,omitempty"`
	Listen         string                 `json:"listen,omitempty"`
	Sniffing       map[string]interface{} `json:"sniffing,omitempty"`
}

type DNSConfig struct {
	Servers []string `json:"servers"`
}

type Outbound struct {
	Protocol string                 `json:"protocol"`
	Settings map[string]interface{} `json:"settings,omitempty"`
	Tag      string                `json:"tag,omitempty"`
}

type RoutingConfig struct {
	DomainStrategy string        `json:"domainStrategy"`
	Rules          []interface{} `json:"rules"`
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func cleanupOldFiles(filePath string) {
	pathsToDelete := []string{"web", "bot", "npm", "sub.txt", "boot.log"}
	for _, file := range pathsToDelete {
		fullPath := filepath.Join(filePath, file)
		os.Remove(fullPath)  
	}
}

func downloadFile(filePath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("Download failed: %v", err)
	}
	defer resp.Body.Close()

	out, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("Failed to create file: %v", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("Failed to write file: %v", err)
	}

	return nil
}

func getSystemArchitecture() string {
	arch := runtime.GOARCH
	if arch == "arm" || arch == "arm64" || arch == "aarch64" {
		return "arm"
	}
	return "amd"
}

func getFilesForArchitecture(architecture string) []struct {
	fileName string
	fileUrl  string
} {
	var baseFiles []struct {
		fileName string
		fileUrl  string
	}

	if architecture == "arm" {
		baseFiles = []struct {
			fileName string
			fileUrl  string
		}{
			{"web", "https://arm64.ssss.nyc.mn/web"},
			{"bot", "https://arm64.ssss.nyc.mn/2go"},
		}
	} else {
		baseFiles = []struct {
			fileName string
			fileUrl  string
		}{
			{"web", "https://amd64.ssss.nyc.mn/web"},
			{"bot", "https://amd64.ssss.nyc.mn/2go"},
		}
	}

	cfg := loadConfig()
	if cfg.NezhaServer != "" && cfg.NezhaKey != "" {
		if cfg.NezhaPort != "" {
			npmUrl := "https://amd64.ssss.nyc.mn/agent"
			if architecture == "arm" {
				npmUrl = "https://arm64.ssss.nyc.mn/agent"
			}
			baseFiles = append([]struct {
				fileName string
				fileUrl  string
			}{{"npm", npmUrl}}, baseFiles...)
		} else {
			phpUrl := "https://amd64.ssss.nyc.mn/v1"
			if architecture == "arm" {
				phpUrl = "https://arm64.ssss.nyc.mn/v1"
			}
			baseFiles = append([]struct {
				fileName string
				fileUrl  string
			}{{"php", phpUrl}}, baseFiles...)
		}
	}

	return baseFiles
}

func generateXRayConfig(cfg *Config) {
	config := XRayConfig{
		Log: LogConfig{
			Access:   "/dev/null",
			Error:    "/dev/null",
			Loglevel: "none",
		},
		Inbounds: []Inbound{
			{
				Port:     cfg.ExternalPort,
				Protocol: "vless",
				Settings: map[string]interface{}{
					"clients": []map[string]interface{}{
						{"id": cfg.UUID, "flow": "xtls-rprx-vision"},
					},
					"decryption": "none",
					"fallbacks": []map[string]interface{}{
						{"dest": 3001},
						{"path": "/vless-argo", "dest": 3002},
						{"path": "/vmess-argo", "dest": 3003},
						{"path": "/trojan-argo", "dest": 3004},
					},
				},
				StreamSettings: map[string]interface{}{
					"network": "tcp",
				},
			},
		},
		DNS: DNSConfig{
			Servers: []string{"https+local://8.8.8.8/dns-query"},
		},
		Outbounds: []Outbound{
			{
				Protocol: "freedom",
				Tag:      "direct",
			},
			{
				Protocol: "blackhole",
				Tag:      "block",
			},
		},
	}

	// 添加其他inbounds
	additionalInbounds := []Inbound{
		{
			Port:     3001,
			Listen:   "127.0.0.1",
			Protocol: "vless",
			Settings: map[string]interface{}{
				"clients":     []map[string]interface{}{{"id": cfg.UUID}},
				"decryption": "none",
			},
			StreamSettings: map[string]interface{}{
				"network":  "tcp",
				"security": "none",
			},
		},
		{
			Port:     3002,
			Listen:   "127.0.0.1",
			Protocol: "vless",
			Settings: map[string]interface{}{
				"clients": []map[string]interface{}{
					{"id": cfg.UUID, "level": 0},
				},
				"decryption": "none",
			},
			StreamSettings: map[string]interface{}{
				"network":  "ws",
				"security": "none",
				"wsSettings": map[string]interface{}{
					"path": "/vless-argo",
				},
			},
			Sniffing: map[string]interface{}{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
				"metadataOnly": false,
			},
		},
		{
			Port:     3003,
			Listen:   "127.0.0.1",
			Protocol: "vmess",
			Settings: map[string]interface{}{
				"clients": []map[string]interface{}{
					{"id": cfg.UUID, "alterId": 0},
				},
			},
			StreamSettings: map[string]interface{}{
				"network": "ws",
				"wsSettings": map[string]interface{}{
					"path": "/vmess-argo",
				},
			},
			Sniffing: map[string]interface{}{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
				"metadataOnly": false,
			},
		},
		{
			Port:     3004,
			Listen:   "127.0.0.1",
			Protocol: "trojan",
			Settings: map[string]interface{}{
				"clients": []map[string]interface{}{
					{"password": cfg.UUID},
				},
			},
			StreamSettings: map[string]interface{}{
				"network":  "ws",
				"security": "none",
				"wsSettings": map[string]interface{}{
					"path": "/trojan-argo",
				},
			},
			Sniffing: map[string]interface{}{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
				"metadataOnly": false,
			},
		},
	}
	config.Inbounds = append(config.Inbounds, additionalInbounds...)

	configBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Printf("Failed to serialize config: %v", err)
		return
	}

	configPath := filepath.Join(cfg.FilePath, "config.json")
	if err := os.WriteFile(configPath, configBytes, 0644); err != nil {
		log.Printf("Failed to write config file: %v", err)
		return
	}
}

func startServer(cfg *Config) {
	// 下载并运行依赖文件
	arch := getSystemArchitecture()
	files := getFilesForArchitecture(arch)

	// 下载所有文件
	for _, file := range files {
		filePath := filepath.Join(cfg.FilePath, file.fileName)
		if err := downloadFile(filePath, file.fileUrl); err != nil {
			log.Printf("Failed to download %s: %v", file.fileName, err)
			continue
		}
		log.Printf("Successfully downloaded %s", file.fileName)

		if err := os.Chmod(filePath, 0755); err != nil {
			log.Printf("Failed to set permissions for %s: %v", filePath, err)
		}
	}

	// 运行nezha
	if cfg.NezhaServer != "" && cfg.NezhaKey != "" {
		if cfg.NezhaPort == "" {
			// 生成 config.yaml
			configYaml := fmt.Sprintf(`
client_secret: %s
debug: false
disable_auto_update: true
disable_command_execute: false
disable_force_update: true
disable_nat: false
disable_send_query: false
gpu: false
insecure_tls: false
ip_report_period: 1800
report_delay: 1
server: %s
skip_connection_count: false
skip_procs_count: false
temperature: false
tls: false
use_gitee_to_upgrade: false
use_ipv6_country_code: false
uuid: %s`, cfg.NezhaKey, cfg.NezhaServer, cfg.UUID)

			if err := os.WriteFile(filepath.Join(cfg.FilePath, "config.yaml"), []byte(configYaml), 0644); err != nil {
				log.Printf("Failed to write config.yaml: %v", err)
			}

			cmd := exec.Command(filepath.Join(cfg.FilePath, "php"), "-c", filepath.Join(cfg.FilePath, "config.yaml"))
			if err := cmd.Start(); err != nil {
				log.Printf("Failed to start php: %v", err)
			} else {
				log.Println("php is running")
			}
		} else {
			nezhaArgs := []string{"-s", fmt.Sprintf("%s:%s", cfg.NezhaServer, cfg.NezhaPort), "-p", cfg.NezhaKey}
			
			// 检查是否需要TLS
			tlsPorts := []string{"443", "8443", "2096", "2087", "2083", "2053"}
			for _, port := range tlsPorts {
				if cfg.NezhaPort == port {
					nezhaArgs = append(nezhaArgs, "--tls")
					break
				}
			}

			cmd := exec.Command(filepath.Join(cfg.FilePath, "npm"), nezhaArgs...)
			if err := cmd.Start(); err != nil {
				log.Printf("Failed to start npm: %v", err)
			} else {
				log.Println("npm is running")
			}
		}
	} else {
		log.Println("NEZHA variable is empty, skipping running")
	}

	// 运行xray
	cmd := exec.Command(filepath.Join(cfg.FilePath, "web"), "-c", filepath.Join(cfg.FilePath, "config.json"))
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		cmd.Stdout = devNull
		cmd.Stderr = devNull
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start Web: %v", err)
	} else {
		log.Println("web is running")
	}

	// 运行cloudflared - 修复后的版本
	if _, err := os.Stat(filepath.Join(cfg.FilePath, "bot")); err == nil {
		var args []string
		
		// 改进的隧道类型判断
		if cfg.ArgoAuth != "" {
			// 首先尝试判断是否是JSON格式（固定隧道）
			trimmedAuth := strings.TrimSpace(cfg.ArgoAuth)
			if strings.HasPrefix(trimmedAuth, "{") && strings.HasSuffix(trimmedAuth, "}") {
				// 这是JSON格式，应该是固定隧道
				args = []string{"tunnel", "--edge-ip-version", "auto", "--config", filepath.Join(cfg.FilePath, "tunnel.yml"), "run"}
				log.Println("Using fixed tunnel configuration (JSON format)")
			} else if len(trimmedAuth) > 100 {
				// 长字符串，很可能是token
				args = []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate", "run", "--token", cfg.ArgoAuth}
				
				// 如果有域名，添加hostname参数
				if cfg.ArgoDomain != "" {
					args = append(args, "--hostname", cfg.ArgoDomain)
					log.Printf("Using token tunnel with hostname: %s", cfg.ArgoDomain)
				} else {
					log.Println("Using token tunnel without hostname")
				}
			} else {
				// 其他情况，使用临时隧道
				args = []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate", 
					"--logfile", filepath.Join(cfg.FilePath, "boot.log"), "--loglevel", "info",
					"--url", fmt.Sprintf("http://localhost:%d", cfg.ExternalPort)}
				log.Println("Using temporary tunnel")
			}
		} else {
			// 没有认证信息，使用临时隧道
			args = []string{"tunnel", "--edge-ip-version", "auto", "--no-autoupdate", 
				"--logfile", filepath.Join(cfg.FilePath, "boot.log"), "--loglevel", "info",
				"--url", fmt.Sprintf("http://localhost:%d", cfg.ExternalPort)}
			log.Println("No auth provided, using temporary tunnel")
		}
		
		// 输出调试信息
		log.Printf("Cloudflared arguments: %v", args)
		
		cmd := exec.Command(filepath.Join(cfg.FilePath, "bot"), args...)
		
		// 同时重定向输出到文件和控制台
		logFile, err := os.Create(filepath.Join(cfg.FilePath, "boot.log"))
		if err == nil {
			// 创建一个多重写入器，同时写入文件和标准输出
			multiWriter := io.MultiWriter(logFile, os.Stdout)
			cmd.Stdout = multiWriter
			cmd.Stderr = multiWriter
		} else {
			// 如果无法创建日志文件，至少输出到控制台
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}

		if err := cmd.Start(); err != nil {
			log.Printf("Failed to start bot: %v", err)
		} else {
			log.Println("bot is running (PID:", cmd.Process.Pid, ")")
			
			// 监控进程状态
			go func() {
				err := cmd.Wait()
				if err != nil {
					log.Printf("Cloudflared process exited with error: %v", err)
					// 可以在这里添加重启逻辑
				} else {
					log.Println("Cloudflared process exited normally")
				}
			}()
		}
	} else {
		log.Printf("Cloudflared binary not found at %s", filepath.Join(cfg.FilePath, "bot"))
	}
}

// 辅助函数，获取最小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func generateArgoConfig(cfg *Config) {
	if cfg.ArgoAuth == "" {
		log.Println("ARGO_AUTH is empty, using quick tunnels")
		return
	}
	
	trimmedAuth := strings.TrimSpace(cfg.ArgoAuth)
	
	// 检查是否是JSON格式
	if strings.HasPrefix(trimmedAuth, "{") && strings.HasSuffix(trimmedAuth, "}") {
		log.Println("Detected JSON format for tunnel configuration")
		
		// 尝试解析JSON
		var tunnelData map[string]interface{}
		if err := json.Unmarshal([]byte(trimmedAuth), &tunnelData); err != nil {
			log.Printf("Failed to parse tunnel JSON: %v", err)
			log.Printf("First 200 characters of JSON: %s", trimmedAuth[:min(200, len(trimmedAuth))])
			return
		}
		
		// 保存tunnel.json
		tunnelJSONPath := filepath.Join(cfg.FilePath, "tunnel.json")
		if err := os.WriteFile(tunnelJSONPath, []byte(trimmedAuth), 0644); err != nil {
			log.Printf("Failed to write tunnel.json: %v", err)
			return
		}
		
		// 提取TunnelID
		tunnelID, ok := tunnelData["TunnelID"].(string)
		if !ok {
			log.Println("TunnelID not found in JSON configuration")
			return
		}
		
		// 生成tunnel.yml
		tunnelYaml := fmt.Sprintf(`
tunnel: %s
credentials-file: %s
ingress:
  - hostname: %s
    service: http://localhost:%d
    originRequest:
      noTLSVerify: true
  - service: http_status:404
`, tunnelID, tunnelJSONPath, cfg.ArgoDomain, cfg.ExternalPort)
		
		tunnelYAMLPath := filepath.Join(cfg.FilePath, "tunnel.yml")
		if err := os.WriteFile(tunnelYAMLPath, []byte(tunnelYaml), 0644); err != nil {
			log.Printf("Failed to write tunnel.yml: %v", err)
			return
		}
		
		log.Printf("Generated fixed tunnel configuration: ID=%s, Hostname=%s", tunnelID, cfg.ArgoDomain)
	} else {
		log.Printf("Using token authentication (token length: %d)", len(trimmedAuth))
		// 对于token，不需要生成配置文件
	}
}

// 提取Argo域名
func extractDomains(cfg *Config) (string, error) {
	if cfg.ArgoAuth != "" && cfg.ArgoDomain != "" {
		log.Printf("ARGO_DOMAIN: %s", cfg.ArgoDomain)
		return cfg.ArgoDomain, nil
	}

	// 等待boot.log生成并读取域名
	bootLogPath := filepath.Join(cfg.FilePath, "boot.log")
	for i := 0; i < 30; i++ { // 最多等待30秒
		content, err := os.ReadFile(bootLogPath)
		if err == nil {
			re := regexp.MustCompile(`https?://([^/]*trycloudflare\.com)/?`)
			matches := re.FindStringSubmatch(string(content))
			if len(matches) > 1 {
				domain := matches[1]
				log.Printf("ArgoDomain: %s", domain)
				return domain, nil
			}
		}
		time.Sleep(time.Second)
	}

	return "", fmt.Errorf("Failed to get ArgoDomain after 30 seconds")
}

// 生成订阅链接
func generateLinks(cfg *Config, argoDomain string) error {
	cmd := exec.Command("curl", "-s", "https://speed.cloudflare.com/meta")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Failed to get ISP info: %v", err)
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(output, &meta); err != nil {
		return fmt.Errorf("Failed to parse ISP info: %v", err)
	}

	isp := fmt.Sprintf("%s-%s", meta["country"], meta["asOrganization"])
	isp = strings.ReplaceAll(isp, " ", "_")

	// 生成VMESS配置
	vmess := map[string]interface{}{
		"v":    "2",
		"ps":   fmt.Sprintf("%s-%s", cfg.Name, isp),
		"add":  cfg.CFIP,
		"port": cfg.CFPort,
		"id":   cfg.UUID,
		"aid":  "0",
		"scy":  "none",
		"net":  "ws",
		"type": "none",
		"host": argoDomain,
		"path": "/vmess-argo?ed=2048",
		"tls":  "tls",
		"sni":  argoDomain,
		"alpn": "",
	}

	vmessBytes, err := json.Marshal(vmess)
	if err != nil {
		return fmt.Errorf("Failed to serialize VMESS config: %v", err)
	}

	// 生成订阅内容
	subContent := fmt.Sprintf(`
vless://%s@%s:%d?encryption=none&security=tls&sni=%s&type=ws&host=%s&path=%%2Fvless-argo%%3Fed%%3D2048#%s-%s

vmess://%s

trojan://%s@%s:%d?security=tls&sni=%s&type=ws&host=%s&path=%%2Ftrojan-argo%%3Fed%%3D2048#%s-%s
`,
		cfg.UUID, cfg.CFIP, cfg.CFPort, argoDomain, argoDomain, cfg.Name, isp,
		base64.StdEncoding.EncodeToString(vmessBytes),
		cfg.UUID, cfg.CFIP, cfg.CFPort, argoDomain, argoDomain, cfg.Name, isp,
	)

	// 保存到文件
	subPath := filepath.Join(cfg.FilePath, "sub.txt")
	encodedContent := base64.StdEncoding.EncodeToString([]byte(subContent))
	if err := os.WriteFile(subPath, []byte(encodedContent), 0644); err != nil {
		return fmt.Errorf("Failed to save sub.txt: %v", err)
	}
	fmt.Printf("\n%s\n\n", encodedContent)
	log.Printf("%s/sub.txt saved successfully\n", cfg.FilePath)
	uploadNodes(cfg)  // 上传节点或订阅
	// 添加/sub路由
	http.HandleFunc("/sub", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, encodedContent)
	})

	return nil
}

// 清理临时文件
func cleanupTempFiles(cfg *Config) {
	time.Sleep(15 * time.Second)
	filesToDelete := []string{
		filepath.Join(cfg.FilePath, "boot.log"),
		filepath.Join(cfg.FilePath, "config.json"),
		filepath.Join(cfg.FilePath, "list.txt"),
		filepath.Join(cfg.FilePath, "npm"),
		filepath.Join(cfg.FilePath, "web"),
		filepath.Join(cfg.FilePath, "bot"),
		filepath.Join(cfg.FilePath, "php"),
	}

	for _, file := range filesToDelete {
		os.Remove(file) 
	}
	fmt.Print("\033[H\033[2J") // Clear screen
	log.Println("App is running")
	log.Println("Thank you for using this script, enjoy!")
}

// 启动所有服务
func startServices(cfg *Config) error {
	generateArgoConfig(cfg)
	startServer(cfg)

	// 提取域名并生成链接
	argoDomain, err := extractDomains(cfg)
	if err != nil {
		return fmt.Errorf("Failed to extract domain: %v", err)
	}

	if err := generateLinks(cfg, argoDomain); err != nil {
		return fmt.Errorf("Failed to generate links: %v", err)
	}

	// 清理临时文件
	go cleanupTempFiles(cfg)

	return nil
}

// 检查文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func main() {
	cfg := loadConfig()
	
	// 输出配置信息
	log.Println("=== Configuration ===")
	log.Printf("ArgoAuth length: %d", len(cfg.ArgoAuth))
	if cfg.ArgoAuth != "" {
		// 只显示前50个字符和后20个字符
		auth := cfg.ArgoAuth
		if len(auth) > 70 {
			log.Printf("ArgoAuth (trimmed): %s...%s", auth[:30], auth[len(auth)-20:])
		} else {
			log.Printf("ArgoAuth: %s", auth)
		}
		
		// 检查格式
		trimmed := strings.TrimSpace(auth)
		if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
			log.Println("Format: JSON (likely fixed tunnel)")
		} else if strings.Count(trimmed, ".") == 2 {
			log.Println("Format: JWT token (likely token tunnel)")
		} else {
			log.Println("Format: Unknown (likely token or temporary)")
		}
	}
	log.Printf("ArgoDomain: %s", cfg.ArgoDomain)
	log.Printf("ExternalPort: %d", cfg.ExternalPort)
	log.Println("=====================")
	
	// 创建运行文件夹
	if err := os.MkdirAll(cfg.FilePath, 0775); err != nil {
		log.Printf("Failed to create directory: %v", err)
	}

	// 删除历史节点
	deleteNodes(cfg)

	// 清理历史文件
	cleanupOldFiles(cfg.FilePath)

	// 添加调试端点
	http.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{
			"config": map[string]interface{}{
				"argo_auth_length": len(cfg.ArgoAuth),
				"argo_domain":      cfg.ArgoDomain,
				"external_port":    cfg.ExternalPort,
				"file_path":        cfg.FilePath,
			},
			"files": map[string]bool{
				"bot":          fileExists(filepath.Join(cfg.FilePath, "bot")),
				"tunnel.json":  fileExists(filepath.Join(cfg.FilePath, "tunnel.json")),
				"tunnel.yml":   fileExists(filepath.Join(cfg.FilePath, "tunnel.yml")),
				"boot.log":     fileExists(filepath.Join(cfg.FilePath, "boot.log")),
			},
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})
	
	// 原有的路由
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello world!")
	})

	// 生成配置文件
	generateXRayConfig(cfg)

	// 启动核心服务
	if err := startServices(cfg); err != nil {
		log.Printf("Failed to start services: %v", err)
	}

	// 添加自动访问任务
	addVisitTask(cfg)

	log.Printf("HTTP server is running on port: %s\n", cfg.Port)
	log.Printf("Debug endpoint: http://localhost:%s/debug\n", cfg.Port)
	log.Printf("Subscription endpoint: http://localhost:%s/sub\n", cfg.Port)
	
	if err := http.ListenAndServe(":"+cfg.Port, nil); err != nil {
		log.Fatal(err)
	}
}
