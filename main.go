package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

type Metrics struct {
	CPU        float64 `json:"cpu"`
	RAM        float64 `json:"ram"`
	RAMUsedMB  uint64  `json:"ramUsedMb"`
	RAMTotalMB uint64  `json:"ramTotalMb"`
	GPU        float64 `json:"gpu"`
}

type AuthMessage struct {
	Secret string `json:"secret"`
}

var (
	apiURL = getEnv("M_WSS_URL", "")
	secret = getEnv("M_AGENT_SECRET", "")
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	for {
		err := run()
		if err != nil {
			log.Printf("Connection error: %v. Reconnecting in 5s...", err)
		}
		time.Sleep(5 * time.Second)
	}
}

func run() error {
	log.Printf("Connecting to %s...", apiURL)

	conn, _, err := websocket.DefaultDialer.Dial(apiURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	auth := AuthMessage{Secret: secret}
	if err := conn.WriteJSON(auth); err != nil {
		return fmt.Errorf("auth send: %w", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("auth response: %w", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(msg, &resp); err != nil {
		return fmt.Errorf("auth parse: %w", err)
	}
	if resp["status"] != "ok" {
		return fmt.Errorf("auth failed: %s", resp["status"])
	}

	log.Println("Authenticated successfully")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	if err := sendMetrics(conn); err != nil {
		return err
	}

	for range ticker.C {
		if err := sendMetrics(conn); err != nil {
			return err
		}
	}

	return nil
}

func sendMetrics(conn *websocket.Conn) error {
	metrics := collectMetrics()

	if err := conn.WriteJSON(metrics); err != nil {
		return fmt.Errorf("send metrics: %w", err)
	}

	log.Printf("CPU: %.1f%% | RAM: %.1f%% (%d MB / %d MB) | GPU: %.0f%%",
		metrics.CPU, metrics.RAM, metrics.RAMUsedMB, metrics.RAMTotalMB, metrics.GPU)

	return nil
}

func collectMetrics() Metrics {
	m := Metrics{GPU: -1}

	cpuPercent, err := cpu.Percent(0, false)
	if err == nil && len(cpuPercent) > 0 {
		m.CPU = cpuPercent[0]
	}

	memStat, err := mem.VirtualMemory()
	if err == nil {
		m.RAM = memStat.UsedPercent
		m.RAMUsedMB = memStat.Used / 1024 / 1024
		m.RAMTotalMB = memStat.Total / 1024 / 1024
	}

	if gpuUsage, ok := getNvidiaGPU(); ok {
		m.GPU = gpuUsage
	}

	return m
}

func getNvidiaGPU() (float64, bool) {
	cmd := exec.Command(
		"nvidia-smi",
		"--query-gpu=utilization.gpu",
		"--format=csv,noheader,nounits",
	)

	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		return 0, false
	}

	value := strings.TrimSpace(out.String())
	usage, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}

	return usage, true
}
