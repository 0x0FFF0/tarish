package cpu

import (
	"bufio"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// Info holds CPU detection results
type Info struct {
	Model    string
	Family   string // e.g., "apple_m3", "intel", "amd"
	Cores    int
	Arch     string // "arm64" or "amd64"
	OS       string // "darwin" or "linux"
	RawModel string // Original unprocessed model string
}

// Detect detects CPU information for the current system
func Detect() (*Info, error) {
	info := &Info{
		Cores: runtime.NumCPU(),
		Arch:  runtime.GOARCH,
		OS:    runtime.GOOS,
	}

	var err error
	switch runtime.GOOS {
	case "darwin":
		err = detectDarwin(info)
	case "linux":
		err = detectLinux(info)
	default:
		info.Model = "unknown"
		info.Family = "unknown"
	}

	if err != nil {
		return nil, err
	}

	info.Family = determineFamily(info.Model)
	return info, nil
}

// detectDarwin detects CPU on macOS
func detectDarwin(info *Info) error {
	// Try to get Apple Silicon model first
	if info.Arch == "arm64" {
		// Get chip name using sysctl
		out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
		if err == nil && len(out) > 0 {
			info.RawModel = strings.TrimSpace(string(out))
			info.Model = normalizeModel(info.RawModel)
			return nil
		}

		// Fallback: try hw.model for Apple Silicon
		out, err = exec.Command("system_profiler", "SPHardwareDataType").Output()
		if err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if strings.Contains(line, "Chip:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						info.RawModel = strings.TrimSpace(parts[1])
						info.Model = normalizeModel(info.RawModel)
						return nil
					}
				}
			}
		}
	}

	// Intel Mac or fallback
	out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
	if err != nil {
		info.Model = "unknown"
		info.RawModel = "unknown"
		return nil
	}

	info.RawModel = strings.TrimSpace(string(out))
	info.Model = normalizeModel(info.RawModel)
	return nil
}

// detectLinux detects CPU on Linux
func detectLinux(info *Info) error {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		info.Model = "unknown"
		info.RawModel = "unknown"
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				info.RawModel = strings.TrimSpace(parts[1])
				info.Model = normalizeModel(info.RawModel)
				return nil
			}
		}
	}

	info.Model = "unknown"
	info.RawModel = "unknown"
	return nil
}

// normalizeModel converts raw model string to a normalized form
func normalizeModel(raw string) string {
	model := strings.ToLower(raw)
	model = strings.ReplaceAll(model, " ", "_")
	model = strings.ReplaceAll(model, "-", "_")
	model = strings.ReplaceAll(model, "(", "")
	model = strings.ReplaceAll(model, ")", "")
	model = strings.ReplaceAll(model, "@", "")
	model = strings.ReplaceAll(model, ",", "")

	// Remove multiple underscores
	re := regexp.MustCompile(`_+`)
	model = re.ReplaceAllString(model, "_")

	// Trim trailing underscores
	model = strings.Trim(model, "_")

	return model
}

// determineFamily extracts the CPU family from the model
func determineFamily(model string) string {
	modelLower := strings.ToLower(model)

	// Apple Silicon detection
	if strings.Contains(modelLower, "apple") || strings.Contains(modelLower, "m1") ||
		strings.Contains(modelLower, "m2") || strings.Contains(modelLower, "m3") ||
		strings.Contains(modelLower, "m4") {

		// Determine specific Apple chip family
		if strings.Contains(modelLower, "m1") {
			if strings.Contains(modelLower, "ultra") {
				return "apple_m1_ultra"
			}
			if strings.Contains(modelLower, "max") {
				return "apple_m1_max"
			}
			if strings.Contains(modelLower, "pro") {
				return "apple_m1_pro"
			}
			return "apple_m1"
		}
		if strings.Contains(modelLower, "m2") {
			if strings.Contains(modelLower, "ultra") {
				return "apple_m2_ultra"
			}
			if strings.Contains(modelLower, "max") {
				return "apple_m2_max"
			}
			if strings.Contains(modelLower, "pro") {
				return "apple_m2_pro"
			}
			return "apple_m2"
		}
		if strings.Contains(modelLower, "m3") {
			if strings.Contains(modelLower, "ultra") {
				return "apple_m3_ultra"
			}
			if strings.Contains(modelLower, "max") {
				return "apple_m3_max"
			}
			if strings.Contains(modelLower, "pro") {
				return "apple_m3_pro"
			}
			return "apple_m3"
		}
		if strings.Contains(modelLower, "m4") {
			if strings.Contains(modelLower, "ultra") {
				return "apple_m4_ultra"
			}
			if strings.Contains(modelLower, "max") {
				return "apple_m4_max"
			}
			if strings.Contains(modelLower, "pro") {
				return "apple_m4_pro"
			}
			return "apple_m4"
		}
		return "apple"
	}

	// Intel detection
	if strings.Contains(modelLower, "intel") {
		if strings.Contains(modelLower, "xeon") {
			return "intel_xeon"
		}
		if strings.Contains(modelLower, "core") {
			// Try to extract generation
			if strings.Contains(modelLower, "i9") {
				return "intel_i9"
			}
			if strings.Contains(modelLower, "i7") {
				return "intel_i7"
			}
			if strings.Contains(modelLower, "i5") {
				return "intel_i5"
			}
			if strings.Contains(modelLower, "i3") {
				return "intel_i3"
			}
		}
		return "intel"
	}

	// AMD detection
	if strings.Contains(modelLower, "amd") {
		if strings.Contains(modelLower, "ryzen") {
			if strings.Contains(modelLower, "threadripper") {
				return "amd_threadripper"
			}
			// Check for specific Ryzen 9 models first
			if strings.Contains(modelLower, "9950x") {
				return "9950x"
			}
			if strings.Contains(modelLower, "9900x") {
				return "9900x"
			}
			if strings.Contains(modelLower, "7950x") {
				return "7950x"
			}
			if strings.Contains(modelLower, "5950x") {
				return "5950x"
			}
			if strings.Contains(modelLower, "5900x") {
				return "5900x"
			}
			// Generic Ryzen families
			if strings.Contains(modelLower, "9") {
				return "amd_ryzen9"
			}
			if strings.Contains(modelLower, "7") {
				return "amd_ryzen7"
			}
			if strings.Contains(modelLower, "5") {
				return "amd_ryzen5"
			}
			return "amd_ryzen"
		}
		if strings.Contains(modelLower, "epyc") {
			return "amd_epyc"
		}
		return "amd"
	}

	return "generic"
}

// GetConfigName returns the suggested config filename for this CPU
func (i *Info) GetConfigName() string {
	return i.Family + ".json"
}

// String returns a human-readable representation
func (i *Info) String() string {
	return i.RawModel + " (" + i.Family + ", " + string(rune(i.Cores)) + " cores, " + i.Arch + ")"
}
