// Package sshlinux discovers Linux hosts through audited SSH commands.
package sshlinux

const (
	CommandHostname   = "hostname"
	CommandMachineID  = "cat /etc/machine-id"
	CommandOSRelease  = "cat /etc/os-release"
	CommandArch       = "uname -m"
	CommandCPUCount   = "getconf _NPROCESSORS_ONLN"
	CommandMemoryMB   = "awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo"
	CommandSerial     = "cat /sys/class/dmi/id/product_serial"
	CommandInterfaces = "ip -j address show"
	CommandPackages   = "if command -v dpkg-query >/dev/null 2>&1; then dpkg-query -W -f='${Package}\\n'; elif command -v rpm >/dev/null 2>&1; then rpm -qa --qf '%{NAME}\\n'; fi"
	CommandServices   = "systemctl list-units --type=service --all --no-legend --no-pager"
)

var AllowedCommands = []string{
	CommandHostname,
	CommandMachineID,
	CommandOSRelease,
	CommandArch,
	CommandCPUCount,
	CommandMemoryMB,
	CommandSerial,
	CommandInterfaces,
	CommandPackages,
	CommandServices,
}

func IsAllowedCommand(command string) bool {
	for _, allowed := range AllowedCommands {
		if command == allowed {
			return true
		}
	}
	return false
}
