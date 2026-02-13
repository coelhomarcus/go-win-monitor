package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

//go:embed app.ico
var iconData []byte

type Metrics struct {
	CPU        float64 `json:"cpu"`
	RAM        float64 `json:"ram"`
	RAMUsedMB  uint64  `json:"ramUsedMb"`
	RAMTotalMB uint64  `json:"ramTotalMb"`
	GPU        float64 `json:"gpu"`
}

var (
	apiURL = getEnv("M_API_URL", "")
	secret = getEnv("M_AGENT_SECRET", "")
)

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
	menuStatus = systray.AddMenuItem("Status: Starting...", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit the application")

	menuCPU.Disable()
	menuRAM.Disable()
	menuGPU.Disable()
	menuStatus.Disable()

	go func() {
		endpoint := strings.TrimRight(apiURL, "/") + "/pc-stats/report"
		log.Printf("Reporting to %s", endpoint)

		for {
			metrics := collectMetrics()
			updateMenuMetrics(metrics)

			if err := sendMetrics(endpoint, metrics); err != nil {
				log.Printf("Report error: %v", err)
				setStatus("Error")
			} else {
				setStatus("Connected")
			}

			time.Sleep(30 * time.Second)
		}
	}()

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

func sendMetrics(endpoint string, metrics Metrics) error {
	body, err := json.Marshal(metrics)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
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
