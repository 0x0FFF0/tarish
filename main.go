package main

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"strings"

	"tarish/cpu"
	"tarish/embedded"
	"tarish/install"
	"tarish/service"
	"tarish/update"
	"tarish/xmrig"
)

//go:embed bin configs
var assets embed.FS

// Version is set at build time
var Version = "dev"

func main() {
	// Initialize embedded assets
	embedded.Assets = assets

	// Set version for update package
	update.Version = Version

	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	command := strings.ToLower(os.Args[1])

	switch command {
	case "install", "i":
		handleInstall()
	case "uninstall", "un":
		handleUninstall()
	case "update", "u":
		handleUpdate()
	case "start", "st":
		handleStart()
	case "stop", "sp":
		handleStop()
	case "status":
		handleStatus()
	case "service":
		handleService()
	case "help", "h", "-h", "--help":
		printHelp()
	case "version", "v", "-v", "--version":
		printVersion()
	case "info":
		handleInfo()
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printHelp()
		os.Exit(1)
	}
}

func handleInstall() {
	if err := install.Install(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func handleUninstall() {
	fmt.Print("Are you sure you want to uninstall tarish? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "y" && response != "yes" {
		fmt.Println("Uninstall cancelled")
		return
	}

	if err := install.Uninstall(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func handleUpdate() {
	if err := update.Update(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func handleStart() {
	// Check for --force flag
	force := false
	for _, arg := range os.Args[2:] {
		if arg == "--force" || arg == "-f" {
			force = true
			break
		}
	}

	// Check if already running
	if pid, running := xmrig.IsRunning(); running && !force {
		fmt.Printf("xmrig is already running (PID: %d)\n", pid)
		fmt.Print("Kill and restart? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response != "y" && response != "yes" {
			fmt.Println("Start cancelled")
			return
		}
		force = true
	}

	// Detect CPU and get appropriate config
	fmt.Println("Detecting CPU...")
	cpuInfo, err := cpu.Detect()
	if err != nil {
		fmt.Printf("Error detecting CPU: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  CPU: %s\n", cpuInfo.RawModel)
	fmt.Printf("  Family: %s\n", cpuInfo.Family)
	fmt.Printf("  Cores: %d\n", cpuInfo.Cores)
	fmt.Printf("  Arch: %s/%s\n", cpuInfo.OS, cpuInfo.Arch)

	// Find config
	configsPath := xmrig.GetInstalledConfigPath()
	configPath, err := xmrig.SelectConfig(cpuInfo, configsPath)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println("\nAvailable configs:")
		configs, _ := xmrig.ListAvailableConfigs()
		for _, c := range configs {
			fmt.Printf("  - %s\n", c)
		}
		os.Exit(1)
	}
	fmt.Printf("  Config: %s\n", configPath)

	// Find binary
	binaryInfo, err := xmrig.GetInstalledBinaryPath()
	if err != nil {
		fmt.Printf("Error finding xmrig binary: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  XMRig: %s (v%s)\n", binaryInfo.Path, binaryInfo.Version)

	// Prepare runtime config with api.id and worker-id
	runtimeConfigPath, err := xmrig.PrepareRuntimeConfig(configPath, cpuInfo)
	if err != nil {
		fmt.Printf("Warning: Failed to prepare runtime config, using original: %v\n", err)
		runtimeConfigPath = configPath
	} else {
		fmt.Printf("  Worker: api.id and worker-id assigned\n")
	}

	// Start xmrig
	fmt.Println("\nStarting xmrig...")
	if err := xmrig.Start(binaryInfo.Path, runtimeConfigPath, force); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func handleStop() {
	if err := xmrig.Stop(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func handleStatus() {
	// ANSI color codes
	cyan := "\033[36m"
	yellow := "\033[33m"
	green := "\033[32m"
	red := "\033[31m"
	gray := "\033[90m"
	bold := "\033[1m"
	reset := "\033[0m"

	status, err := xmrig.Status()
	if err != nil {
		fmt.Printf("%sError: %v%s\n", red, err, reset)
		os.Exit(1)
	}

	fmt.Printf("\n%s%s=== Tarish Status ===%s\n\n", bold, cyan, reset)
	fmt.Print(status.FormatStatus())

	// Show service status
	serviceStatus := service.GetServiceStatus()
	serviceColor := green
	serviceHint := ""
	if strings.Contains(strings.ToLower(serviceStatus), "disabled") ||
		strings.Contains(strings.ToLower(serviceStatus), "not") {
		serviceColor = red
		serviceHint = fmt.Sprintf(" %s(run 'sudo tarish service enable')%s", gray, reset)
	}
	fmt.Printf("\n  %sAuto-start:       %s%s%s%s%s\n\n",
		yellow, reset, serviceColor, serviceStatus, reset, serviceHint)
}

func handleService() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: tarish service <enable|disable|status>")
		os.Exit(1)
	}

	subcommand := strings.ToLower(os.Args[2])

	switch subcommand {
	case "enable":
		if err := service.Enable(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "disable", "stop":
		if err := service.Disable(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "status":
		enabled, err := service.IsEnabled()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		if enabled {
			fmt.Println("Auto-start service is enabled")
		} else {
			fmt.Println("Auto-start service is disabled")
		}
	default:
		fmt.Printf("Unknown service command: %s\n", subcommand)
		fmt.Println("Usage: tarish service <enable|disable|status>")
		os.Exit(1)
	}
}

func handleInfo() {
	// Print system info
	fmt.Println("=== System Information ===")
	fmt.Println()

	cpuInfo, err := cpu.Detect()
	if err != nil {
		fmt.Printf("Error detecting CPU: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("CPU Model:  %s\n", cpuInfo.RawModel)
	fmt.Printf("CPU Family: %s\n", cpuInfo.Family)
	fmt.Printf("Cores:      %d\n", cpuInfo.Cores)
	fmt.Printf("OS/Arch:    %s/%s\n", cpuInfo.OS, cpuInfo.Arch)
	fmt.Println()

	// Show expected config
	configsPath := xmrig.GetInstalledConfigPath()
	configPath, err := xmrig.SelectConfig(cpuInfo, configsPath)
	if err != nil {
		fmt.Printf("Config:     (no matching config found)\n")
	} else {
		fmt.Printf("Config:     %s\n", configPath)
	}

	// Show xmrig binary
	binaryInfo, err := xmrig.GetInstalledBinaryPath()
	if err != nil {
		fmt.Printf("XMRig:      (not found)\n")
	} else {
		fmt.Printf("XMRig:      %s (v%s)\n", binaryInfo.Path, binaryInfo.Version)
	}

	// Show installation status
	fmt.Println()
	if install.IsInstalled() {
		fmt.Printf("Installed:  %s\n", install.GetInstallPath())
	} else {
		fmt.Println("Installed:  No (run 'tarish install' to install)")
	}

	// Show available configs
	fmt.Println()
	fmt.Println("Available configs:")
	configs, err := xmrig.ListAvailableConfigs()
	if err != nil {
		fmt.Println("  (none found)")
	} else {
		for _, c := range configs {
			fmt.Printf("  - %s\n", c)
		}
	}
}

func printHelp() {
	// ANSI color codes
	cyan := "\033[36m"
	yellow := "\033[33m"
	green := "\033[32m"
	gray := "\033[90m"
	bold := "\033[1m"
	reset := "\033[0m"

	fmt.Printf(`
%s%starish%s - XMRig Wrapper for Easy Mining

%sUSAGE:%s
    tarish <command> [options]

%sCOMMANDS:%s
    %sinstall, i%s       Install tarish to /usr/local/bin
    %suninstall, un%s    Uninstall tarish from the system
    %supdate, u%s        Update tarish to latest version

    %sstart, st%s        Start mining with auto-detected config
                     %sUse --force to kill existing process%s
    %sstop, sp%s         Stop all xmrig processes
    %sstatus%s           Show mining status and statistics

    %sservice enable%s   Enable auto-start on boot
    %sservice disable%s  Disable auto-start on boot
    %sservice status%s   Show auto-start status

    %sinfo%s             Show system and configuration info
    %shelp, h%s          Show this help message
    %sversion, v%s       Show version information

%sEXAMPLES:%s
    %starish start%s           Start mining
    %starish start --force%s   Force restart mining
    %starish stop%s            Stop mining
    %starish status%s          Check mining status
    
    %ssudo tarish install%s    Install to system
    %ssudo tarish service enable%s   Enable auto-start

%sFor more information, visit: https://file.aooo.nl/tarish/%s
`,
		bold, cyan, reset,
		yellow, reset,
		yellow, reset,
		green, reset,
		green, reset,
		green, reset,
		green, reset,
		gray, reset,
		green, reset,
		green, reset,
		green, reset,
		green, reset,
		green, reset,
		green, reset,
		green, reset,
		green, reset,
		yellow, reset,
		cyan, reset,
		cyan, reset,
		cyan, reset,
		cyan, reset,
		cyan, reset,
		cyan, reset,
		gray, reset,
	)
}

func printVersion() {
	fmt.Printf("tarish version %s\n", Version)
}
