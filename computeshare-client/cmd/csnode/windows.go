//go:build windows

// Windows layer (node card 003): csnode.exe checks prerequisites, installs WSL2 and the
// csnode-linux distro, then forwards every subcommand into the distro, where the Linux
// csnode + Incus do the real work.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alishafiee1/computeshare-client/internal/release"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const distro = "csnode-linux"

// exitRebootNeeded: `csnode install` exit code meaning "WSL was just installed, restart Windows first".
const exitRebootNeeded = 10

var version = "dev"

func die(code int, f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...); os.Exit(code) }

func wsl(a ...string) *exec.Cmd {
	c := exec.Command("wsl.exe", a...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c
}

func distroInstalled() bool {
	out, _ := exec.Command("wsl.exe", "-l", "-q").Output()
	// wsl -l prints UTF-16LE
	s := strings.ReplaceAll(string(out), "\x00", "")
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) == distro {
			return true
		}
	}
	return false
}

// R1
func preflight() {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err == nil {
		b, _, _ := k.GetStringValue("CurrentBuildNumber")
		if n, _ := strconv.Atoi(b); n > 0 && n < 19044 {
			die(1, "Windows 10 21H2 or newer is required (build %d found)", n)
		}
		k.Close()
	}
	out, _ := exec.Command("powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_Processor).VirtualizationFirmwareEnabled -contains $true -or (Get-CimInstance Win32_ComputerSystem).HypervisorPresent").Output()
	if !strings.Contains(strings.ToLower(string(out)), "true") {
		die(1, "virtualization is disabled in BIOS")
	}
}

// elevated reports whether this process runs with Administrator rights (UAC "run as administrator").
func elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// startCmd is what keeps the distro (and so the node) alive after boot or logon.
func startCmd() string {
	return `wsl.exe -d ` + distro + ` -u root -- sh -c "systemctl start csnode; exec sleep infinity"`
}

// R6: .wslconfig limits vs requested
func wslconfigCheck(memGiB, cpus int) {
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".wslconfig")
	f, err := os.Open(p)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(strings.ToUpper(v))
		if k == "memory" && memGiB > 0 {
			n, _ := strconv.Atoi(strings.TrimSuffix(v, "GB"))
			if n > 0 && n < memGiB {
				fmt.Printf("warning: %s memory=%s is lower than the space limit %dGiB; raise it and run `wsl --shutdown`\n", p, v, memGiB)
			}
		}
		if k == "processors" && cpus > 0 {
			if n, _ := strconv.Atoi(v); n > 0 && n < cpus {
				fmt.Printf("warning: %s processors=%d is lower than the space limit %d\n", p, n, cpus)
			}
		}
	}
}

// wslDataDir is where the Linux system file (the WSL virtual disk, with the node's space inside) lives.
// The installer asks for it (so it can sit on a roomy drive such as D:); the default is under ProgramData on C:.
func wslDataDir(arg string) (string, error) {
	if arg == "" {
		return filepath.Join(os.Getenv("ProgramData"), "csnode", "wsl"), nil
	}
	if strings.HasPrefix(arg, `\\`) || !filepath.IsAbs(arg) {
		return "", fmt.Errorf(`the data folder must be on a local drive, like D:\ComputeShare (got %q)`, arg)
	}
	return filepath.Clean(arg), nil
}

// R2/R3: install WSL + import distro from the mirror rootfs
func install(rootfs, dataDir string) {
	preflight()
	dir, err := wslDataDir(dataDir)
	if err != nil {
		die(1, "%v", err)
	}
	// card 012 R5/R6: the rootfs must be listed in the signed SHA256SUMS bundled next to it
	if err := release.VerifyFile(rootfs); err != nil {
		die(1, "امضای نسخه معتبر نیست؛ نصب متوقف شد. فایل را فقط از منبع رسمی دریافت کنید.\n(%v)", err)
	}
	if exec.Command("wsl.exe", "--status").Run() != nil {
		// turning on the WSL Windows features is the one step that really needs Administrator rights
		if !elevated() {
			die(1, "WSL is not installed and installing it needs Administrator rights. Run the installer once as administrator (or install WSL yourself with: wsl --install --no-distribution, restart, then run this installer again)")
		}
		if err := wsl("--install", "--no-distribution").Run(); err != nil {
			die(1, "wsl install failed: %v", err)
		}
		// resume after reboot
		exe, _ := os.Executable()
		hive := "HKCU"
		if elevated() {
			hive = "HKLM"
		}
		exec.Command("reg", "add", hive+`\SOFTWARE\Microsoft\Windows\CurrentVersion\RunOnce`, "/v", "csnode", "/d", `"`+exe+`" install --rootfs "`+rootfs+`" --wsl-dir "`+dir+`"`, "/f").Run()
		fmt.Println("WSL installed; reboot Windows, then run the installer again to finish.")
		os.Exit(exitRebootNeeded) // the installer wizard shows its restart page on this code
	}
	if !distroInstalled() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			die(1, "cannot create %s: %v", dir, err)
		}
		if err := wsl("--import", distro, dir, rootfs, "--version", "2").Run(); err != nil {
			die(1, "import %s: %v", distro, err)
		}
	}
	serviceInstall()
	fmt.Println("csnode-linux ready. Next: csnode setup")
}

// R4: with Administrator rights: a scheduled task (SYSTEM, at startup) keeps the distro + tunnel alive
// without a logon. Without them (per-user install): a Run entry of the current user, which starts the
// node at every logon of that user.
func serviceInstall() {
	if elevated() {
		exec.Command("schtasks", "/Create", "/F", "/RU", "SYSTEM", "/SC", "ONSTART", "/TN", "csnode", "/TR", startCmd()).Run()
		exec.Command("schtasks", "/Run", "/TN", "csnode").Run()
		return
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		fmt.Println("warning: could not register the logon start entry:", err)
		return
	}
	defer k.Close()
	k.SetStringValue("csnode", `cmd.exe /c start "" /min `+startCmd())
	exec.Command("cmd.exe", "/c", "start", "", "/min", "wsl.exe", "-d", distro, "-u", "root", "--", "sh", "-c", "systemctl start csnode; exec sleep infinity").Start()
}

// R7
func purge() {
	wsl("-d", distro, "-u", "root", "--", "csnode", "leave", "--purge", "--yes").Run()
	exec.Command("schtasks", "/Delete", "/F", "/TN", "csnode").Run()
	if k, err := registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE); err == nil {
		k.DeleteValue("csnode")
		k.Close()
	}
	wsl("--unregister", distro).Run()
	fmt.Println("csnode removed (other WSL distros untouched)")
}

func main() {
	a := os.Args[1:]
	if len(a) > 0 {
		switch a[0] {
		case "install":
			rootfs, dataDir := "csnode-linux-rootfs.tar.gz", ""
			for i := 1; i+1 < len(a); i += 2 {
				switch a[i] {
				case "--rootfs":
					rootfs = a[i+1]
				case "--wsl-dir":
					dataDir = a[i+1]
				}
			}
			install(rootfs, dataDir)
			return
		case "version", "--version":
			fmt.Println("csnode", version, "(windows)")
			return
		case "uninstall":
			purge()
			return
		case "setup":
			mem, cpu := 0, 0
			for i := 1; i+1 < len(a); i++ {
				switch a[i] {
				case "--memory":
					mem, _ = strconv.Atoi(strings.TrimRight(a[i+1], "GiB"))
				case "--cpu":
					cpu, _ = strconv.Atoi(a[i+1])
				}
			}
			wslconfigCheck(mem, cpu)
			for _, x := range a {
				if x == "--vm" {
					die(1, "VM mode is not available on WSL2")
				}
			}
		}
	}
	if !distroInstalled() {
		die(1, "csnode-linux is not installed; run: csnode install")
	}
	c := wsl(append([]string{"-d", distro, "-u", "root", "--", "csnode"}, a...)...)
	if err := c.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			os.Exit(e.ExitCode())
		}
		die(1, "%v", err)
	}
}
