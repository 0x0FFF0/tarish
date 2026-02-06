# Tarish - XMRig Wrapper for Easy Mining

Tarish is a user-friendly wrapper for XMRig that simplifies cryptocurrency mining on macOS and Linux systems. It provides automatic CPU detection, configuration management, and 24/7 operation support with built-in sleep prevention.

## Features

- 🚀 **Easy Setup**: One-command installation and configuration
- 🔍 **Auto CPU Detection**: Automatically detects your CPU and selects optimal settings
- 📦 **Embedded Binaries**: XMRig binaries and configs included - no separate downloads needed
- 🔧 **Configuration Management**: Pre-configured profiles for popular CPUs (M1, M2, M3, M4, Ryzen, etc.)
- 🚫 **Zero Donation**: Completely donation-free mining
- 🌙 **Sleep Prevention**: Automatic system wake-lock for 24/7 operation
- 🔄 **Auto-start**: Optional service installation for automatic startup on boot
- 📊 **Status Monitoring**: Real-time hashrate and pool statistics
- 🔒 **Safe Process Management**: Clean process handling with PID tracking

## Supported Systems

### macOS
- Apple Silicon: M1, M1 Pro, M2 Max, M3, M3 Pro, M3 Max, M4, M4 Pro
- Intel: macOS 10.13+

### Linux
- x86_64: Ubuntu, Debian, Fedora, CentOS, Arch, etc.
- ARM64: Raspberry Pi, ARM servers

## Quick Start

### Installation

```bash
# Clone or download tarish
git clone https://github.com/yourusername/tarish.git
cd tarish

# Install to system (requires sudo)
sudo ./tarish install

# Or install to user directory (no sudo needed)
./tarish install
```

### Basic Usage

```bash
# Start mining (auto-detects CPU and config)
tarish start

# Check mining status
tarish status

# Stop mining
tarish stop
```

### Auto-start on Boot

```bash
# Enable auto-start service (requires sudo on Linux)
sudo tarish service enable

# Check service status
tarish service status

# Disable auto-start
sudo tarish service disable
```

## Sleep Prevention

Tarish automatically prevents your system from sleeping during mining operations to ensure 24/7 uptime.

### How It Works

#### macOS
Uses the built-in `caffeinate` command to prevent:
- System idle sleep
- Display sleep
- Disk idle sleep

#### Linux
Uses `systemd-inhibit` (modern systems) or fallback methods to prevent:
- System idle sleep
- Suspend/hibernate
- Lid-close sleep

### Status

Check if sleep prevention is active:

```bash
tarish status
```

Output will show:
```
Sleep Prevention: ACTIVE ✓
```

### Manual Testing

#### macOS
```bash
# View active power assertions
pmset -g assertions | grep PreventUserIdleSystemSleep

# Check caffeinate process
ps aux | grep caffeinate
```

#### Linux
```bash
# List active sleep inhibitors
systemd-inhibit --list

# Check for tarish inhibitor
systemd-inhibit --list | grep tarish
```

## Commands

### Core Commands

| Command | Alias | Description |
|---------|-------|-------------|
| `install` | `i` | Install tarish to system |
| `uninstall` | `un` | Remove tarish from system |
| `update` | `u` | Update to latest version |
| `start` | `st` | Start mining |
| `stop` | `sp` | Stop mining |
| `status` | - | Show mining status |
| `info` | - | Show system information |

### Service Commands

| Command | Description |
|---------|-------------|
| `service enable` | Enable auto-start on boot |
| `service disable` | Disable auto-start |
| `service status` | Check auto-start status |

### Options

| Option | Description |
|--------|-------------|
| `--force` or `-f` | Kill existing process and restart |

## CPU Configurations

Tarish includes optimized configurations for popular CPUs:

### Apple Silicon
- M1 (`m1.json`)
- M1 Pro (`m1pro.json`)
- M2 Max (`m2max.json`)
- M3 (`m3.json`)
- M3 Pro (`m3pro.json`)
- M3 Max (`m3max.json`)
- M4 (`m4.json`)
- M4 Pro (`m4pro.json`)

### AMD Ryzen
- Ryzen 9 5900X (`5900x.json`)
- Ryzen 9 7950X (`7950x.json`)
- Ryzen 9 9950X (`9950x.json`)

## Examples

### Start Mining

```bash
# Basic start (auto-detect CPU)
tarish start

# Force restart if already running
tarish start --force
```

### Check Status

```bash
tarish status
```

Output example:
```
=== Tarish Status ===

Status: RUNNING (PID: 12345)
Version: 6.25.0
Uptime: 2h 15m
Hashrate: 8234.56 H/s (10s) | 8156.78 H/s (60s) | 8456.90 H/s (max)
Pool: pool.supportxmr.com:443
Wallet: 4AdUndXH...
Donate Level: 0%
Sleep Prevention: ACTIVE ✓

Auto-start: enabled
```

### System Info

```bash
tarish info
```

Output example:
```
=== System Information ===

CPU Model:  Apple M3 Max
CPU Family: apple_m3_max
Cores:      16
OS/Arch:    darwin/arm64

Config:     /usr/local/share/tarish/configs/m3max.json
XMRig:      /usr/local/share/tarish/bin/6.25.0/xmrig_macos_arm64 (v6.25.0)

Installed:  /usr/local/bin/tarish

Available configs:
  - m1.json
  - m1pro.json
  - m2max.json
  - m3.json
  - m3max.json
  - m3pro.json
  - m4.json
  - m4pro.json
  - 5900x.json
  - 7950x.json
  - 9950x.json
```

## Installation Locations

### System Installation (with sudo)
- Binary: `/usr/local/bin/tarish`
- Data: `/usr/local/share/tarish/`
- Configs: `/usr/local/share/tarish/configs/`
- Logs: `/usr/local/share/tarish/log/`
- Service: 
  - macOS: `/Library/LaunchDaemons/com.tarish.plist`
  - Linux: `/etc/systemd/system/tarish.service`

### User Installation (without sudo)
- Binary: `~/.local/bin/tarish`
- Data: `~/.local/share/tarish/`
- Configs: `~/.local/share/tarish/configs/`
- Logs: `~/.local/share/tarish/log/`
- Service:
  - macOS: `~/Library/LaunchAgents/com.tarish.plist`
  - Linux: User must use system installation for auto-start

## Building from Source

### Prerequisites
- Go 1.21 or later

### Build

```bash
# Clone repository
git clone https://github.com/yourusername/tarish.git
cd tarish

# Build for current platform
go build -o tarish

# Or use the build script
./build.sh
```

### Cross-compilation

```bash
# Build for Linux AMD64
GOOS=linux GOARCH=amd64 go build -o tarish_linux_amd64

# Build for macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o tarish_macos_arm64

# Build for Linux ARM64
GOOS=linux GOARCH=arm64 go build -o tarish_linux_arm64
```

## Troubleshooting

### Mining doesn't start
1. Check if tarish is installed: `which tarish`
2. Check CPU detection: `tarish info`
3. Verify config exists for your CPU
4. Check logs: `cat /usr/local/share/tarish/log/xmrig.log`

### Sleep prevention not working
**macOS:**
```bash
# Check if caffeinate is running
ps aux | grep caffeinate

# Verify power assertions
pmset -g assertions
```

**Linux:**
```bash
# Check systemd-inhibit
systemd-inhibit --list

# If not available, install systemd
sudo apt install systemd  # Debian/Ubuntu
sudo yum install systemd  # RHEL/CentOS
```

### Service won't start on boot

**macOS:**
```bash
# Check service status
launchctl list | grep tarish

# Manually load service
sudo launchctl load -w /Library/LaunchDaemons/com.tarish.plist
```

**Linux:**
```bash
# Check service status
sudo systemctl status tarish

# View logs
sudo journalctl -u tarish

# Manually enable
sudo systemctl enable tarish
sudo systemctl start tarish
```

### Permission denied errors
- Ensure tarish binary is executable: `chmod +x tarish`
- For system installation, use sudo: `sudo tarish install`
- Check log file permissions: `ls -l /usr/local/share/tarish/log/`

## Architecture

```
tarish/
├── main.go              # CLI entry point
├── cpu/                 # CPU detection logic
│   └── detect.go
├── xmrig/              # XMRig process management
│   ├── binary.go       # Binary path resolution
│   ├── config.go       # Configuration selection
│   └── process.go      # Process lifecycle
├── antisleep/          # Sleep prevention
│   ├── antisleep.go    # Cross-platform implementation
│   └── README.md       # Detailed documentation
├── service/            # Auto-start service
│   └── service.go
├── install/            # Installation logic
│   └── install.go
├── update/             # Self-update functionality
│   └── update.go
└── embedded/           # Embedded assets
    └── assets.go
```

## Security Considerations

1. **No Network Access During Build**: XMRig binaries are embedded in the repository
2. **PID Files**: Process tracking uses PID files with appropriate permissions
3. **Log Files**: Logs are world-readable but only writable by the process owner
4. **Service Installation**: Requires sudo/root for system-level auto-start
5. **Sleep Prevention**: Uses OS-native APIs, no privileged access required

## FAQ

**Q: Is this mining software legal?**
A: Yes, cryptocurrency mining is legal in most jurisdictions when done on your own hardware. Always check local regulations.

**Q: Will this damage my computer?**
A: No. XMRig is designed to use CPU resources safely. However, extended high-load operation may increase wear on cooling systems.

**Q: Can I use this on a laptop?**
A: Yes, but be aware of increased heat generation and battery drain. Sleep prevention works on AC power but may be limited on battery.

**Q: How do I change the mining pool or wallet?**
A: Edit the config file in `/usr/local/share/tarish/configs/` and restart mining.

**Q: Does this support GPU mining?**
A: No, Tarish only supports CPU mining via XMRig.

**Q: Why is donate level 0%?**
A: This is a donate-free fork of XMRig. We believe in giving miners full control of their hashrate.

## Contributing

Contributions are welcome! Please:
1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

This project uses XMRig which is licensed under GPL-3.0. See XMRig's license for details.

## Credits

- XMRig: The underlying mining software
- CPU Detection: Based on CPUID and system info APIs
- Sleep Prevention: Uses OS-native power management APIs

## Support

For issues, questions, or feature requests:
- Open an issue on GitHub
- Check existing documentation in the `/antisleep/` directory
- Review the troubleshooting section above

---

**Happy Mining! 🚀**
