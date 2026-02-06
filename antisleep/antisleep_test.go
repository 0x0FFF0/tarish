package antisleep

import (
	"runtime"
	"testing"
	"time"
)

func TestEnableDisable(t *testing.T) {
	// Skip on unsupported platforms
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unsupported platform")
	}

	// Test initial state
	if IsEnabled() {
		t.Fatal("Sleep prevention should not be enabled initially")
	}

	// Test Enable
	err := Enable()
	if err != nil {
		t.Fatalf("Failed to enable sleep prevention: %v", err)
	}

	// Give the process a moment to start
	time.Sleep(100 * time.Millisecond)

	// Check if enabled
	if !IsEnabled() {
		t.Fatal("Sleep prevention should be enabled after Enable()")
	}

	// Test idempotent Enable
	err = Enable()
	if err != nil {
		t.Fatalf("Enable should be idempotent: %v", err)
	}

	// Test Disable
	err = Disable()
	if err != nil {
		t.Fatalf("Failed to disable sleep prevention: %v", err)
	}

	// Give the process a moment to stop
	time.Sleep(100 * time.Millisecond)

	// Check if disabled
	if IsEnabled() {
		t.Fatal("Sleep prevention should be disabled after Disable()")
	}

	// Test idempotent Disable
	err = Disable()
	if err != nil {
		t.Fatalf("Disable should be idempotent: %v", err)
	}
}

func TestMultipleEnableDisable(t *testing.T) {
	// Skip on unsupported platforms
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unsupported platform")
	}

	// Enable and disable multiple times
	for i := 0; i < 3; i++ {
		err := Enable()
		if err != nil {
			t.Fatalf("Enable failed on iteration %d: %v", i, err)
		}

		time.Sleep(50 * time.Millisecond)

		if !IsEnabled() {
			t.Fatalf("Should be enabled on iteration %d", i)
		}

		err = Disable()
		if err != nil {
			t.Fatalf("Disable failed on iteration %d: %v", i, err)
		}

		time.Sleep(50 * time.Millisecond)

		if IsEnabled() {
			t.Fatalf("Should be disabled on iteration %d", i)
		}
	}
}

// TestPlatformSpecific tests platform-specific implementations
func TestPlatformSpecific(t *testing.T) {
	guard := &Guard{}

	switch runtime.GOOS {
	case "darwin":
		err := guard.enableMacOS()
		if err != nil {
			t.Fatalf("Failed to enable macOS sleep prevention: %v", err)
		}
		defer guard.stop()

		if !guard.active {
			t.Fatal("Guard should be active after enableMacOS()")
		}

		// Verify caffeinate process is running
		if guard.cmd == nil || guard.cmd.Process == nil {
			t.Fatal("caffeinate process should be running")
		}

	case "linux":
		err := guard.enableLinux()
		if err != nil {
			t.Fatalf("Failed to enable Linux sleep prevention: %v", err)
		}
		defer guard.stop()

		if !guard.active {
			t.Fatal("Guard should be active after enableLinux()")
		}

		// Verify process is running
		if guard.cmd == nil || guard.cmd.Process == nil {
			t.Fatal("systemd-inhibit or fallback process should be running")
		}

	default:
		t.Skip("Unsupported platform")
	}
}

// Benchmark for Enable operation
func BenchmarkEnable(b *testing.B) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		b.Skip("Unsupported platform")
	}

	for i := 0; i < b.N; i++ {
		Enable()
		Disable()
	}
}
