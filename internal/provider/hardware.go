package provider

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// detectHardware returns a human-readable hardware description string.
// Format: "CPU model, RAM, GPU" or fallback to hostname-only on failure.
func detectHardware() string {
	var parts []string

	// CPU.
	if info, err := cpu.Info(); err == nil && len(info) > 0 {
		name := strings.TrimSpace(info[0].ModelName)
		if name != "" {
			parts = append(parts, name)
		}
	}

	// RAM.
	if v, err := mem.VirtualMemory(); err == nil && v.Total > 0 {
		gb := v.Total / (1024 * 1024 * 1024)
		if gb > 0 {
			parts = append(parts, formatRAM(gb))
		}
	}

	// GPU detection.
	if gpu := detectGPU(); gpu != "" {
		parts = append(parts, gpu)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

// formatRAM formats RAM in GB with a nice suffix.
func formatRAM(gb uint64) string {
	if gb >= 1024 {
		return "1TB+ RAM"
	}
	return formatUint(gb) + "GB RAM"
}

// formatUint is a simple uint64-to-string helper (avoiding fmt for deps).
func formatUint(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// detectGPU tries nvidia-smi, then wmic (Windows), then /sys/class/drm/ (Linux), then macOS system_profiler.
func detectGPU() string {
	switch runtime.GOOS {
	case "linux":
		return detectGPULinux()
	case "darwin":
		return detectGPUDarwin()
	case "windows":
		return detectGPUWindows()
	default:
		return ""
	}
}

func detectGPULinux() string {
	// Try nvidia-smi.
	if out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits").Output(); err == nil {
		line := strings.TrimSpace(string(out))
		if line != "" {
			fields := strings.SplitN(line, ",", 2)
			if len(fields) == 2 {
				name := strings.TrimSpace(fields[0])
				memMB := strings.TrimSpace(fields[1])
				memGB := ""
				if mb, ok := parseUint(memMB); ok && mb > 0 {
					memGB = " " + formatUint(mb/1024) + "GB"
				}
				return name + memGB
			}
		}
	}
	return ""
}

func detectGPUDarwin() string {
	// Use system_profiler for Apple Silicon / AMD GPUs.
	out, err := exec.Command("system_profiler", "SPDisplaysDataType").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Chipset Model:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Chipset Model:"))
		}
	}
	return ""
}

// detectGPUWindows detects GPU on Windows using wmic.
func detectGPUWindows() string {
	// Try nvidia-smi first (may be in PATH for NVIDIA users).
	if out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits").Output(); err == nil {
		line := strings.TrimSpace(string(out))
		if line != "" {
			fields := strings.SplitN(line, ",", 2)
			if len(fields) == 2 {
				name := strings.TrimSpace(fields[0])
				memMB := strings.TrimSpace(fields[1])
				if mb, ok := parseUint(memMB); ok && mb > 0 {
					return name + " " + formatUint(mb/1024) + "GB"
				}
				return name
			}
		}
	}
	// Fall back to wmic for AMD/Intel GPUs.
	out, err := exec.Command("wmic", "path", "win32_videocontroller", "get", "name").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.EqualFold(trimmed, "name") {
			continue
		}
		return trimmed
	}
	return ""
}

func parseUint(s string) (uint64, bool) {
	var n uint64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, true
}
