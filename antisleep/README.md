# Anti-Sleep Package

This package prevents system sleep on macOS and Linux to ensure 24/7 operation during mining.

## How It Works

### macOS
Uses the `caffeinate` command with the following flags:
- `-d`: Prevent display from sleeping
- `-i`: Prevent system from idle sleeping  
- `-m`: Prevent disk from idle sleeping

The `caffeinate` process runs in the background and keeps the system awake as long as it's active.

### Linux

#### Primary Method: systemd-inhibit (Recommended)
Uses `systemd-inhibit` to block system sleep through the systemd login manager:
- Inhibits: `idle`, `sleep`, and `handle-lid-switch`
- Mode: `block` (completely prevents sleep)
- Works on all modern Linux distributions with systemd

#### Fallback Method: Legacy Systems
For systems without systemd:
- Disables X11 display power management using `xset` (if available)
- Keeps a background process running

## Usage

```go
import "tarish/antisleep"

// Enable sleep prevention
if err := antisleep.Enable(); err != nil {
    log.Printf("Failed to enable sleep prevention: %v", err)
}

// Check if enabled
if antisleep.IsEnabled() {
    fmt.Println("Sleep prevention is active")
}

// Disable sleep prevention
if err := antisleep.Disable(); err != nil {
    log.Printf("Failed to disable sleep prevention: %v", err)
}
```

## Integration

The antisleep package is automatically integrated with the xmrig process lifecycle:

1. **On Start**: Sleep prevention is enabled when `tarish start` is executed
2. **On Stop**: Sleep prevention is disabled when `tarish stop` is executed or when the mining process exits
3. **Status**: The `tarish status` command shows whether sleep prevention is currently active

## Requirements

### macOS
- `caffeinate` (built-in on all macOS systems)

### Linux
- `systemd-inhibit` (included in systemd, available on most modern distributions)
- Fallback: `xset` (optional, for X11 display management)

## Permissions

- **macOS**: No special permissions required
- **Linux**: No root permissions required when using systemd-inhibit (works in user session)

## Testing

To verify sleep prevention is working:

### macOS
```bash
# Check if caffeinate is running
ps aux | grep caffeinate

# Check system power assertions
pmset -g assertions | grep PreventUserIdleSystemSleep
```

### Linux
```bash
# Check systemd inhibitors
systemd-inhibit --list

# Or use D-Bus to query inhibitors
busctl --user call org.freedesktop.login1 /org/freedesktop/login1 \
  org.freedesktop.login1.Manager ListInhibitors
```

## Behavior

- Sleep prevention is tied to the xmrig process lifecycle
- If the xmrig process crashes or is killed forcefully, the cleanup goroutine will disable sleep prevention
- Multiple enable calls are safe (idempotent)
- Disable is safe to call even if not enabled

## Limitations

1. **macOS Battery Power**: The `-s` flag (prevent sleep on lid close) only works when connected to AC power. On battery, closing the lid will still sleep the system, but idle sleep is prevented.

2. **Linux Legacy Systems**: On systems without systemd, the fallback method is best-effort and may not prevent all types of sleep.

3. **SSH Sessions**: When running via SSH, ensure your session doesn't timeout. Consider using `screen` or `tmux` for persistent sessions.

## Troubleshooting

### macOS: "caffeinate: command not found"
This should never happen on macOS as caffeinate is built-in. If it does, your system installation may be corrupted.

### Linux: "systemd-inhibit: command not found"
Your system may not use systemd. The package will fall back to legacy methods automatically.

### Sleep prevention not working on Linux
1. Check if systemd is running: `systemctl --version`
2. Check if inhibitors are supported: `systemd-inhibit --list`
3. Ensure you're running in a login session (not just SSH without systemd-logind)

### Process cleanup issues
If the antisleep process doesn't terminate properly:
```bash
# macOS
pkill -f caffeinate

# Linux  
pkill -f systemd-inhibit
```

## Future Enhancements

Potential improvements for future versions:
- Add Windows support using `SetThreadExecutionState` API
- Implement D-Bus inhibit directly (without systemd-inhibit command)
- Add wake-on-LAN support for remote wake-up
- Implement network activity monitoring to prevent network sleep
- Add configurable sleep prevention modes (display-only, system-only, etc.)
