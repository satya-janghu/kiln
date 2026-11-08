# kiln

Sets up a fresh Debian 13 (trixie) amd64 install in one go: packages, SSH, firewall, Docker, Cockpit, developer tools, drivers and more. Run it once, right after the Debian installer, at the machine's console as root:

```bash
wget -qO- https://tinyurl.com/kilnsetup | bash
```

With sudo already working, pipe to `sudo bash` instead. The script checks that the machine runs Debian 13 amd64, downloads the latest release, verifies its checksum and starts kiln. kiln shows one checklist (everything ticked), runs the items one by one, and ends with a summary. It is not installed and keeps nothing afterwards except its log, `/var/log/kiln.log`.

What each item does: [docs/steps-catalog.md](docs/steps-catalog.md). Why: [docs/decisions.md](docs/decisions.md).
