package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"tarish/cpu"
	"tarish/install"
	"tarish/service"
	"tarish/update"
	"tarish/xmrig"
)

// Version is set at build time
var Version = "dev"

func main() {
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
	binPath := xmrig.GetBinPath()
	binaryInfo, err := xmrig.FindBinary(binPath)
	if err != nil {
		fmt.Printf("Error finding xmrig binary: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  XMRig: %s (v%s)\n", binaryInfo.Path, binaryInfo.Version)

	// Start xmrig
	fmt.Println("\nStarting xmrig...")
	if err := xmrig.Start(binaryInfo.Path, configPath, force); err != nil {
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
	status, err := xmrig.Status()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== Tarish Status ===")
	fmt.Println()
	fmt.Print(status.FormatStatus())

	// Show service status
	serviceStatus := service.GetServiceStatus()
	fmt.Printf("\nAuto-start: %s\n", serviceStatus)
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
	binPath := xmrig.GetBinPath()
	binaryInfo, err := xmrig.FindBinary(binPath)
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
	help := `
tarish - XMRig Wrapper for Easy Mining

USAGE:
    tarish <command> [options]

COMMANDS:
    install, i       Install tarish to /usr/local/bin
    uninstall, un    Uninstall tarish from the system
    update, u        Update tarish from GitHub releases

    start, st        Start mining with auto-detected config
                     Use --force to kill existing process
    stop, sp         Stop all xmrig processes
    status           Show mining status and statistics

    service enable   Enable auto-start on boot
    service disable  Disable auto-start on boot
    service status   Show auto-start status

    info             Show system and configuration info
    help, h          Show this help message
    version, v       Show version information

EXAMPLES:
    tarish start           Start mining
    tarish start --force   Force restart mining
    tarish stop            Stop mining
    tarish status          Check mining status
    
    sudo tarish install    Install to system
    sudo tarish service enable   Enable auto-start

For more information, visit: https://github.com/0x0FFF0/tarish
`
	fmt.Println(help)
}

func printVersion() {
	fmt.Printf("tarish version %s\n", Version)
}
