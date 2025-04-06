package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type NodeStatus struct {
	NodeIP      string             `json:"node_ip"`
	Timestamp   time.Time          `json:"timestamp"`
	Connections []ConnectionStatus `json:"connections"`
}

type ConnectionStatus struct {
	TargetIP    string  `json:"target_ip"`
	Latency     float64 `json:"latency"`
	PacketLoss  float64 `json:"packet_loss"`
	IsConnected bool    `json:"is_connected"`
}

type HistoricalData struct {
	Timestamp   time.Time
	Connections []ConnectionStatus
}

var (
	nodeStatuses = make(map[string]NodeStatus)
	history      = make(map[string][]HistoricalData)
	mutex        sync.RWMutex
)

func main() {
	// 设置路由
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/nodes", handleGetNodes)
	http.HandleFunc("/api/history", handleGetHistory)

	// 限制文件服务器的访问范围
	fs := http.FileServer(http.Dir("web"))
	http.Handle("/", http.StripPrefix("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 禁止访问敏感文件或目录
		sensitivePaths := []string{"/server/", "/config/", "/.env"}
		for _, sensitivePath := range sensitivePaths {
			if strings.HasPrefix(r.URL.Path, sensitivePath) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		fs.ServeHTTP(w, r)
	})))

	// 启动清理过期数据的goroutine
	go cleanupOldData()

	// 启动服务器，增加安全配置
	port := "23480"
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      nil,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Printf("Server starting on port %s", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 限制请求体大小
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	defer r.Body.Close()

	var status NodeStatus
	if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// 验证输入数据
	if !isValidNodeStatus(status) {
		http.Error(w, "Invalid data", http.StatusBadRequest)
		return
	}

	// 存储节点状态和历史数据
	mutex.Lock()
	nodeStatuses[status.NodeIP] = status
	history[status.NodeIP] = append(history[status.NodeIP], HistoricalData{
		Timestamp:   status.Timestamp,
		Connections: status.Connections,
	})
	mutex.Unlock()

	w.WriteHeader(http.StatusOK)
}

// 验证 NodeStatus 数据的完整性和合理性
func isValidNodeStatus(status NodeStatus) bool {
	if !isValidIPv4(status.NodeIP) || len(status.Connections) == 0 {
		return false
	}

	for _, conn := range status.Connections {
		if !isValidConnectionStatus(conn) {
			return false
		}
	}

	return true
}

// 验证 ConnectionStatus 数据的完整性和合理性
func isValidConnectionStatus(conn ConnectionStatus) bool {
	if !isValidIPv4(conn.TargetIP) || conn.Latency < 0 || conn.PacketLoss < 0 || conn.PacketLoss > 100 {
		return false
	}
	return true
}

// 验证是否为合法的 IPv4 地址
func isValidIPv4(ip string) bool {
	return net.ParseIP(ip) != nil && strings.Count(ip, ":") == 0
}

func handleGetNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mutex.RLock()
	defer mutex.RUnlock()

	// 返回所有节点状态
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodeStatuses)
}

func handleGetHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	nodeIP := r.URL.Query().Get("node")
	if nodeIP == "" {
		http.Error(w, "node parameter is required", http.StatusBadRequest)
		return
	}

	// 获取时间范围参数
	startTime := r.URL.Query().Get("start")
	endTime := r.URL.Query().Get("end")

	mutex.RLock()
	defer mutex.RUnlock()

	nodeHistory, exists := history[nodeIP]
	if !exists {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}

	// 过滤时间范围
	var filteredHistory []HistoricalData
	for _, data := range nodeHistory {
		if startTime != "" {
			start, err := time.Parse(time.RFC3339, startTime)
			if err == nil && data.Timestamp.Before(start) {
				continue
			}
		}
		if endTime != "" {
			end, err := time.Parse(time.RFC3339, endTime)
			if err == nil && data.Timestamp.After(end) {
				continue
			}
		}
		filteredHistory = append(filteredHistory, data)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(filteredHistory)
}

func cleanupOldData() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		mutex.Lock()
		now := time.Now()
		// 保留最近24小时的数据
		for nodeIP, nodeHistory := range history {
			var newHistory []HistoricalData
			for _, data := range nodeHistory {
				if now.Sub(data.Timestamp) <= 24*time.Hour {
					newHistory = append(newHistory, data)
				}
			}
			history[nodeIP] = newHistory
		}
		mutex.Unlock()
	}
}
