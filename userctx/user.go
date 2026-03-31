package userctx

import (
	"fmt"
	"os"
	"os/user"
	"strings"
)

// Identity describes the OS user that tarish should use for stateful paths.
type Identity struct {
	Username string
	HomeDir  string
}

// Current resolves the tarish owner account. When invoked via sudo, tarish
// keeps using the original caller's home so rebooted services and interactive
// commands share the same config and runtime state.
func Current() (*Identity, error) {
	if home := strings.TrimSpace(os.Getenv("TARISH_HOME")); home != "" {
		identity := &Identity{HomeDir: home}
		if username := strings.TrimSpace(os.Getenv("TARISH_USER")); username != "" {
			identity.Username = username
		} else if username := strings.TrimSpace(os.Getenv("SUDO_USER")); username != "" {
			identity.Username = username
		}
		return identity, nil
	}

	if os.Geteuid() == 0 {
		if sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER")); sudoUser != "" && sudoUser != "root" {
			identity, err := lookupUser(sudoUser)
			if err == nil {
				return identity, nil
			}
			if home := strings.TrimSpace(os.Getenv("HOME")); home != "" && home != "/root" {
				return &Identity{Username: sudoUser, HomeDir: home}, nil
			}
			return nil, fmt.Errorf("failed to resolve home directory for sudo user %q", sudoUser)
		}
	}

	currentUser, currentErr := user.Current()
	if currentErr == nil && strings.TrimSpace(currentUser.HomeDir) != "" {
		return &Identity{Username: currentUser.Username, HomeDir: currentUser.HomeDir}, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	identity := &Identity{HomeDir: home}
	if currentErr == nil {
		identity.Username = currentUser.Username
	}
	return identity, nil
}

// HomeDir returns the resolved tarish owner home directory.
func HomeDir() (string, error) {
	identity, err := Current()
	if err != nil {
		return "", err
	}
	return identity.HomeDir, nil
}

func lookupUser(username string) (*Identity, error) {
	account, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(account.HomeDir) == "" {
		return nil, fmt.Errorf("user %q has no home directory", username)
	}
	return &Identity{
		Username: account.Username,
		HomeDir:  account.HomeDir,
	}, nil
}
