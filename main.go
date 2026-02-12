package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"github.com/gorilla/websocket"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

//go:embed icon.ico
var iconData []byte

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

// Menu items atualizáveis
var (
	menuCPU    *systray.MenuItem
	menuRAM    *systray.MenuItem
	menuGPU    *systray.MenuItem
	menuStatus *systray.MenuItem
	menuMu     sync.Mutex
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("")
	systray.SetTooltip("Computer Monitor")

	menuCPU = systray.AddMenuItem("CPU: ---", "")
	menuRAM = systray.AddMenuItem("RAM: ---", "")
	menuGPU = systray.AddMenuItem("GPU: ---", "")
	systray.AddSeparator()
	menuStatus = systray.AddMenuItem("Status: Disconnected", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit the application")

	// Desabilitar click nos items de info
	menuCPU.Disable()
	menuRAM.Disable()
	menuGPU.Disable()
	menuStatus.Disable()

	// WebSocket loop em goroutine
	go func() {
		for {
			err := run()
			if err != nil {
				log.Printf("Connection error: %v. Reconnecting in 5s...", err)
				setStatus("Disconnected")
			}
			time.Sleep(5 * time.Second)
		}
	}()

	// Quit handler
	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
}

func onExit() {
	os.Exit(0)
}

func setStatus(status string) {
	menuMu.Lock()
	defer menuMu.Unlock()
	menuStatus.SetTitle(fmt.Sprintf("Status: %s", status))
}

func updateMenuMetrics(m Metrics) {
	menuMu.Lock()
	defer menuMu.Unlock()
	menuCPU.SetTitle(fmt.Sprintf("CPU: %.1f%%", m.CPU))
	menuRAM.SetTitle(fmt.Sprintf("RAM: %.1f%% (%d MB / %d MB)", m.RAM, m.RAMUsedMB, m.RAMTotalMB))
	if m.GPU >= 0 {
		menuGPU.SetTitle(fmt.Sprintf("GPU: %.0f%%", m.GPU))
	} else {
		menuGPU.SetTitle("GPU: N/A")
	}
}

func run() error {
	log.Printf("Connecting to %s...", apiURL)
	setStatus("Connecting...")

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
	setStatus("Connected")

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
	updateMenuMetrics(metrics)

	if err := conn.WriteJSON(metrics); err != nil {
		return fmt.Errorf("send metrics: %w", err)
	}

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
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

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
