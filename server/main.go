package main

import (
	"encoding/json"
	"log"
	"net/http"
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
	http.Handle("/", http.FileServer(http.Dir("web")))

	// 启动清理过期数据的goroutine
	go cleanupOldData()

	// 启动服务器
	port := "23480"
	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var status NodeStatus
	if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
