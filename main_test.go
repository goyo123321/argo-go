package main

import (
	"testing"
	"time"
)

func TestGenerateRandomName(t *testing.T) {
	name := generateRandomName(6)
	if len(name) != 6 {
		t.Errorf("期望随机名称长度6，实际长度: %d", len(name))
	}

	name2 := generateRandomName(6)
	if name == name2 {
		t.Errorf("期望生成不同的随机名称，但得到了相同的: %s", name)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{1 * time.Second, "1秒"},
		{61 * time.Second, "1分钟1秒"},
		{3661 * time.Second, "1小时1分钟1秒"},
	}

	for _, test := range tests {
		result := formatDuration(test.duration)
		if result != test.expected {
			t.Errorf("formatDuration(%v) = %s, 期望 %s", test.duration, result, test.expected)
		}
	}
}

func TestAnalyzeTunnelType(t *testing.T) {
	s := &Server{
		config: &Config{},
	}

	// 测试临时隧道
	s.config.ArgoAuth = ""
	if result := s.analyzeTunnelType(); result != TunnelTypeTemporary {
		t.Errorf("空配置期望临时隧道，实际: %s", result)
	}

	// 测试固定隧道
	s.config.ArgoAuth = `{"TunnelSecret":"test","TunnelID":"test"}`
	if result := s.analyzeTunnelType(); result != TunnelTypeFixed {
		t.Errorf("JSON配置期望固定隧道，实际: %s", result)
	}

	// 测试Token隧道
	s.config.ArgoAuth = "AQEDAHh6eXq1tbW2t7i5vL3AwcHCw8TFxsfIycrLzM3Oz9DR0g=="
	if result := s.analyzeTunnelType(); result != TunnelTypeToken {
		t.Errorf("Token配置期望Token隧道，实际: %s", result)
	}
}
