package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

func main() {
	// 获取当前节点IP
	nodeIP := os.Getenv("NODE_IP")
	if nodeIP == "" {
		log.Fatal("NODE_IP environment variable is required")
	}

	// 获取服务器地址
	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		log.Fatal("SERVER_URL environment variable is required")
	}

	// 定期收集和发送状态
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			status := collectNodeStatus(nodeIP)
			if err := sendStatusToServer(serverURL, status); err != nil {
				log.Printf("Error sending status to server: %v", err)
			}
		}
	}
}

func collectNodeStatus(nodeIP string) NodeStatus {
	if !isValidIPv4(nodeIP) {
		log.Fatalf("Invalid NodeIP format: %s", nodeIP)
	}

	status := NodeStatus{
		NodeIP:      nodeIP,
		Timestamp:   time.Now(),
		Connections: make([]ConnectionStatus, 0),
	}

	// 测试与其他节点的连接
	for i := 1; i <= 10; i++ {
		targetIP := fmt.Sprintf("10.0.0.%d", i)
		if targetIP == nodeIP || !isValidIPv4(targetIP) {
			continue
		}

		latency, packetLoss, isConnected := testConnection(targetIP)
		status.Connections = append(status.Connections, ConnectionStatus{
			TargetIP:    targetIP,
			Latency:     latency,
			PacketLoss:  packetLoss,
			IsConnected: isConnected,
		})
	}

	return status
}

// 验证是否为合法的 IPv4 地址
func isValidIPv4(ip string) bool {
	return net.ParseIP(ip) != nil && strings.Count(ip, ":") == 0
}

func testConnection(targetIP string) (float64, float64, bool) {
	// 使用系统ping测试延迟和丢包
	latency, packetLoss := testPing(targetIP)

	// 使用延迟测试超时判断isConnected
	isConnected := latency < 1000 // 假设 1000ms 是一个合理的阈值

	return latency, packetLoss, isConnected
}

func testPing(targetIP string) (float64, float64) {
	cmd := exec.Command("ping", "-c", "4", targetIP)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("Error running ping: %v", err)
		return 0, 100 // 假设全部丢包
	}

	output := string(out)
	//log.Println(output)

	// 提取延迟
	var latency float64
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "rtt min/avg/max/mdev") {
			parts := strings.Split(line, "/")
			if len(parts) > 1 {
				avgLatencyStr := parts[4]
				avgLatencyStr = strings.ReplaceAll(avgLatencyStr, " ms", "")
				latency, err = strconv.ParseFloat(avgLatencyStr, 64)
				if err != nil {
					log.Printf("Error parsing latency: %v", err)
					latency = 0
				}
				break
			}
		}
	}

	// 提取丢包率
	var packetLoss float64
	for _, line := range lines {
		if strings.Contains(line, "packet loss") {
			parts := strings.Split(line, "%")
			if len(parts) > 0 {
				lossParts := strings.Split(parts[0], " ")
				if len(lossParts) > 0 {
					lossStr := lossParts[len(lossParts)-1]
					packetLoss, err = strconv.ParseFloat(lossStr, 64)
					if err != nil {
						log.Printf("Error parsing packet loss: %v", err)
						packetLoss = 100
					}
					break
				}
			}
		}
	}

	return latency, packetLoss
}

func sendStatusToServer(serverURL string, status NodeStatus) error {
	jsonData, err := json.Marshal(status)
	if err != nil {
		return err
	}

	resp, err := http.Post(serverURL+"/api/status", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status code %d", resp.StatusCode)
	}

	return nil
}
