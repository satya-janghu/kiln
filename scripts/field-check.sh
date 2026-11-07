#!/bin/bash
# kiln field check: READ-ONLY. It changes nothing on this machine.
# It runs kiln's read-only commands (facts, plan, battery status, mirror
# bench without --apply) and records the command outputs kiln relies on
# that aren't verified yet (docs/debian13-notes.md, "U" rows).
#
# Copy it next to a kiln binary on the Debian machine and run it as root from
# that folder:   sudo bash field-check.sh   (or, without sudo:  su - -c "bash $PWD/field-check.sh")
# Result: /tmp/kiln-field-<hostname>.txt (the only file it writes).
# See docs/review-process.md, "Field check".
set -u
cd "$(dirname "$0")" || exit 1
OUT="/tmp/kiln-field-$(hostname).txt"
K=./kiln
exec > >(tee "$OUT") 2>&1

sec() { printf '\n\n===== %s =====\n' "$*"; }
run() {
	printf '\n$ %s\n' "$*"
	timeout 600 "$@"
	printf '[exit %s]\n' "$?"
}
has() { command -v "$1" >/dev/null 2>&1; }

sec "basics"
run date -Is
run uname -a
run cat /etc/os-release
run id
run uptime
run "$K" --version

sec "kiln facts"
run "$K" facts
run "$K" facts --json

sec "kiln plan (preview only; Recorder, nothing changes)"
run "$K" plan

sec "kiln battery status"
run "$K" battery status

sec "packages kiln can declare: candidate in this machine's apt sources"
"$K" catalog packages --all 2>/dev/null | awk -F'\t' '$5=="debian"{print $2"\t"$3}' | sort -u |
	while IFS=$'\t' read -r pkg suite; do
		cand=$(LC_ALL=C apt-cache policy "$pkg" 2>/dev/null | awk '/Candidate:/{print $2}')
		printf '%-40s %-18s %s\n' "$pkg" "$suite" "${cand:-NOT FOUND}"
	done
run grep -rh "^[^#]" /etc/apt/sources.list /etc/apt/sources.list.d/

sec "apt / unattended-upgrades formats"
run apt-config dump APT::Periodic
run apt-config dump Unattended-Upgrade::Origins-Pattern
run systemctl show -p TimersCalendar apt-daily.timer apt-daily-upgrade.timer
run dpkg --audit
run ls -la /var/lib/dpkg/updates

sec "ssh"
run systemctl is-enabled ssh.service ssh.socket
run systemctl is-active ssh.service
run ss -H -ltn
run ss -H -l -t -n -p sport = :22
has sshd && run sshd -T -C "user=$(logname 2>/dev/null || echo satya),host=$(hostname),addr=127.0.0.1"
printf '\n$ echo garbage | ssh-keygen -l -f -\n'
echo garbage | ssh-keygen -l -f -
printf '[exit %s]\n' "$?"
run ssh-keygen -l -f /etc/ssh/ssh_host_ed25519_key.pub
run ls -la /etc/ssh/sshd_config.d/
run grep -n -E "^(Include|Port|PermitRootLogin|PasswordAuthentication|KbdInteractive|UsePAM)" /etc/ssh/sshd_config
run awk -F: '$1=="root"||$1=="satya"{print $1": "substr($2,1,3)"..."}' /etc/shadow

sec "network: ifupdown, DHCP, NetworkManager"
run ip -br addr
run ip -j route show default
run cat /etc/network/interfaces
run ls -la /etc/network/interfaces.d/
run ifquery --state
for i in $(ifquery --list 2>/dev/null | grep -v '^lo$'); do run ifup --no-act -v "$i"; done
run ls -la /sbin/dhclient /sbin/udhcpc /sbin/dhcpcd /usr/sbin/dhcpcd
run pgrep -a dhcpcd
run pgrep -a dhclient
run pgrep -a wpa_supplicant
run grep -n -E "^(duid|clientid|iaid|hostname)" /etc/dhcpcd.conf
run ls -la /var/lib/dhcpcd /var/lib/dhcp
run cat /var/lib/dhcpcd/duid
run dpkg-query -W -f='${Package} ${Version}\n' network-manager ifupdown dhcpcd-base isc-dhcp-client wpasupplicant iw
has nmcli && run nmcli -t -g DEVICE,TYPE,STATE device
has firewall-cmd && run firewall-cmd --state

sec "Wi-Fi link and power saving (diagnosis of the SSH timeouts)"
if has iw; then
	run iw dev
	for w in $(iw dev 2>/dev/null | awk '/Interface/{print $2}'); do
		run iw dev "$w" get power_save
		run iw dev "$w" link
		run readlink -f "/sys/class/net/$w/device/driver"
	done
else
	echo "iw isn't installed"
fi
run cat /proc/net/wireless

sec "sleep and lid (did the machine suspend?)"
run systemd-analyze cat-config systemd/logind.conf
run journalctl --list-boots --no-pager
printf '\n$ journalctl -b (suspend/lid/sleep lines)\n'
journalctl -b -o short-iso --no-pager 2>/dev/null | grep -iE 'suspend|sleep|lid switch|PM: |wake' | tail -40
printf '\n$ journalctl -b (Wi-Fi disconnects)\n'
journalctl -b -o short-iso --no-pager 2>/dev/null | grep -iE 'deauth|disassoc|CTRL-EVENT-DISCONNECTED|link becomes ready|carrier' | tail -30

sec "hardware details for the battery / thermal / NVIDIA code"
run cat /sys/class/dmi/id/sys_vendor /sys/class/dmi/id/product_name /sys/class/dmi/id/product_version /sys/class/dmi/id/board_name /sys/class/dmi/id/chassis_type
for p in /sys/class/power_supply/*; do
	printf '\n-- %s\n' "$p"
	for a in type scope online capacity status charge_control_start_threshold charge_control_end_threshold charge_behaviour charge_types manufacturer model_name; do
		[ -e "$p/$a" ] && printf '%s = %s\n' "$a" "$(cat "$p/$a" 2>/dev/null)"
	done
done
run ls /sys/firmware/acpi/
run cat /sys/firmware/acpi/platform_profile /sys/firmware/acpi/platform_profile_choices
run ls /sys/class/platform-profile/
run cat /sys/devices/system/cpu/amd_pstate/status
run cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_driver /sys/devices/system/cpu/cpu0/cpufreq/energy_performance_preference
run ls /sys/devices/platform/
has lspci && run lspci -nn
has mokutil && run mokutil --sb-state
has tasksel && run tasksel --list-tasks

sec "mirror benchmark (downloads test data, writes nothing)"
run "$K" mirrors bench

sec "done"
echo "Results are in $OUT"
