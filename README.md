# AISwitch

**AI Service Smart Proxy Switch**

AISwitch is a lightweight TCP proxy written in Go. It helps developers access AI services through a remote proxy when direct network connections are unavailable or restricted.

If your AI applications, such as CC-Switch or other AI clients, cannot connect directly to AI service providers because of network restrictions, AISwitch provides a simple solution without requiring you to modify the application's proxy settings.

## 1. How It Works

AISwitch redirects selected AI service domains to a local TCP proxy using the operating system's Hosts file.

Instead of modifying CC-Switch or configuring a proxy separately in every application, you only need to:

1. Have access to a working remote HTTP CONNECT proxy or SOCKS5 proxy.
2. Run AISwitch on your local computer.
3. Add the AI service domains you want to access to your Hosts file, mapping them to `127.0.0.1`.
4. Start AISwitch and select your remote proxy type.

AISwitch automatically extracts the destination hostname from the TLS ClientHello using SNI (Server Name Indication), then establishes a connection to the actual destination through the remote proxy.

### Architecture

```text
+-------------------------------+
|       AI Applications         |
|                               |
|  CC-Switch / Claude Code      |
|  Other AI Clients             |
+---------------+---------------+
                |
                | HTTPS Request
                |
                v
+-------------------------------+
|         Hosts File            |
|                               |
|  api.example.com              |
|       -> 127.0.0.1            |
|  auth.example.com             |
|       -> 127.0.0.1            |
+---------------+---------------+
                |
                | TCP :443
                v
+-------------------------------+
|           AISwitch            |
|                               |
|  1. Receive TCP connection    |
|  2. Extract TLS SNI           |
|  3. Determine target domain   |
|  4. Establish proxy tunnel    |
|  5. Forward original traffic  |
+---------------+---------------+
                |
                | SOCKS5 or HTTP CONNECT
                |
                v
+-------------------------------+
|       Remote Proxy Server     |
|                               |
|  HTTP CONNECT Proxy           |
|              OR               |
|  SOCKS5 Proxy                 |
+---------------+---------------+
                |
                | Internet
                v
+-------------------------------+
|       Actual AI Services      |
|                               |
|  AI API / Authentication      |
|  Model Providers / Endpoints  |
+-------------------------------+
```

## 2. Requirements

You only need:

- A working HTTP proxy that supports the HTTPS `CONNECT` method, or a SOCKS5 proxy.
- AISwitch running on your local computer.
- Administrator or root privileges to modify the Hosts file.
- Permission to listen on local TCP port `443`.

You do not need:

- To modify CC-Switch configuration.
- To configure an HTTP proxy separately in every AI application.
- To install additional TLS certificates.
- To decrypt or intercept HTTPS traffic.
- To install additional software dependencies for AISwitch.

**Important:** A normal HTTP website server is not sufficient. You need an actual HTTP forward proxy supporting `CONNECT`, or a SOCKS5 proxy.

## 3. Download and Build

AISwitch is written in Go and supports Windows, Linux, and macOS.

Build for your current operating system:

```bash
go build -o AISwitch main.go
```

Cross-compile examples:

**Windows 64-bit**

```bash
GOOS=windows GOARCH=amd64 go build -o AISwitch.exe main.go
```

**Linux 64-bit**

```bash
GOOS=linux GOARCH=amd64 go build -o AISwitch main.go
```

**macOS Intel**

```bash
GOOS=darwin GOARCH=amd64 go build -o AISwitch main.go
```

**macOS Apple Silicon**

```bash
GOOS=darwin GOARCH=arm64 go build -o AISwitch main.go
```

## 4. Configure the Remote Proxy

AISwitch supports two remote proxy types.

### Option A: SOCKS5 Proxy

Example:

```bash
AISwitch \
  -listen :443 \
  -target example.com:443 \
  -proxy-type socks5 \
  -socks5 192.168.1.100:1080
```

Replace `192.168.1.100:1080` with your actual SOCKS5 proxy address.

### Option B: HTTP CONNECT Proxy

Example:

```bash
AISwitch \
  -listen :443 \
  -target example.com:443 \
  -proxy-type http \
  -http-proxy 192.168.1.100:7890
```

If authentication is required:

```bash
AISwitch \
  -listen :443 \
  -target example.com:443 \
  -proxy-type http \
  -http-proxy 192.168.1.100:7890 \
  -http-user username \
  -http-pass password
```

The `-target` parameter is the fallback destination. When AISwitch successfully extracts SNI from a TLS ClientHello, it uses the detected hostname with the configured target port instead.

For example, if the client requests `api.example.com`, AISwitch connects to `api.example.com:443` through the selected remote proxy.

## 5. Configure the Hosts File

Add the domains used by your AI services to the local Hosts file.

Each domain should point to:

```text
127.0.0.1
```

For example:

```text
# AISwitch - AI Service Domains

127.0.0.1 api.example.com
127.0.0.1 auth.example.com
127.0.0.1 console.example.com
127.0.0.1 gateway.example.com
```

The domains above are examples. Replace them with the actual hostnames used by your AI service providers.

**Important:**

- Add every required hostname, including API and authentication endpoints.
- Add subdomains individually.
- Hosts files do not support wildcard entries such as `*.example.com`.
- Only redirect domains that should pass through AISwitch.
- Do not redirect unrelated websites.

AISwitch uses SNI to distinguish the actual destination when multiple domains point to the same local IP address.

## 6. Windows Configuration

Applicable to Windows 10 and Windows 11.

The Windows Hosts file is located at:

```text
C:\Windows\System32\drivers\etc\hosts
```

**Administrator privileges are required to modify this file.**

### Step 1: Open Notepad as Administrator

1. Click the Windows Start menu.
2. Search for `Notepad`.
3. Right-click Notepad.
4. Select **Run as administrator**.
5. Click **Yes** when Windows displays the User Account Control prompt.

Do not open Notepad normally. Without administrator privileges, Windows will not allow you to save changes to the Hosts file.

### Step 2: Open the Hosts File

In Notepad:

1. Click **File**.
2. Select **Open**.
3. Navigate to:

```text
C:\Windows\System32\drivers\etc\
```

4. Change the file type filter from `Text Documents (*.txt)` to `All Files (*.*)`.
5. Select the file named `hosts`.
6. Click **Open**.

**Important:** The Hosts file does not have a `.txt` extension. Make sure you open the actual `hosts` file, not a file named `hosts.txt`.

### Step 3: Back Up the Original Hosts File

Before making changes, it is recommended to create a backup.

Open PowerShell as Administrator and execute:

```powershell
Copy-Item `
  -Path "$env:SystemRoot\System32\drivers\etc\hosts" `
  -Destination "$env:SystemRoot\System32\drivers\etc\hosts.backup" `
  -Force
```

This creates:

```text
C:\Windows\System32\drivers\etc\hosts.backup
```

### Step 4: Add AI Service Domains

Scroll to the bottom of the Hosts file.

Add your required domains:

```text
# ==================================================
# AISwitch - AI Service Domains
# ==================================================

127.0.0.1 api.example.com
127.0.0.1 auth.example.com
127.0.0.1 console.example.com
127.0.0.1 gateway.example.com
```

Replace the example domains with the actual domains used by your AI service providers.

You can add multiple domains. Each hostname must have its own entry.

For example:

```text
127.0.0.1 api.example.com
127.0.0.1 auth.example.com
127.0.0.1 api.another-example.com
127.0.0.1 auth.another-example.com
```

Do not add wildcard entries such as:

```text
127.0.0.1 *.example.com
```

Wildcards are not supported by the Windows Hosts file.

### Step 5: Save the Hosts File

In Notepad:

1. Click **File**.
2. Select **Save**.
3. Close Notepad.

If Windows reports that you do not have permission to save the file, close Notepad and reopen it using **Run as administrator**.

### Step 6: Flush the Windows DNS Cache

Open Command Prompt or PowerShell as Administrator:

```cmd
ipconfig /flushdns
```

Expected output:

```text
Windows IP Configuration

Successfully flushed the DNS Resolver Cache.
```

### Step 7: Verify the Hosts Configuration

Open Command Prompt:

```cmd
ping api.example.com
```

The output should show:

```text
Pinging api.example.com [127.0.0.1]
```

Alternatively, use PowerShell:

```powershell
[System.Net.Dns]::GetHostAddresses("api.example.com")
```

The expected result should contain:

```text
127.0.0.1
```

If the result still shows the original public IP address, check the Hosts file entries and flush the DNS cache again.

### Step 8: Restore the Original Hosts File

If you want to undo the changes, open PowerShell as Administrator:

```powershell
Copy-Item `
  -Path "$env:SystemRoot\System32\drivers\etc\hosts.backup" `
  -Destination "$env:SystemRoot\System32\drivers\etc\hosts" `
  -Force
```

Then flush the DNS cache:

```cmd
ipconfig /flushdns
```

## 7. Linux Configuration

Applicable to common Linux distributions, including:

- Ubuntu
- Debian
- Rocky Linux
- Red Hat Enterprise Linux
- CentOS Stream
- Other Linux distributions using `/etc/hosts`

The Linux Hosts file is located at:

```text
/etc/hosts
```

**Root privileges are required to modify this file.**

### Step 1: Open a Terminal

Open your terminal application.

You can also connect to the Linux machine using SSH.

### Step 2: Back Up the Original Hosts File

Before modifying the file, create a backup:

```bash
sudo cp -a /etc/hosts /etc/hosts.backup
```

Verify that the backup exists:

```bash
ls -l /etc/hosts /etc/hosts.backup
```

The original file is preserved in:

```text
/etc/hosts.backup
```

### Step 3: Open the Hosts File

Use `nano`:

```bash
sudo nano /etc/hosts
```

Alternatively, if you prefer Vim:

```bash
sudo vi /etc/hosts
```

Do not edit `/etc/hosts` using a normal user account without root privileges.

### Step 4: Add AI Service Domains

Move to the bottom of the file and add:

```text
# ==================================================
# AISwitch - AI Service Domains
# ==================================================

127.0.0.1 api.example.com
127.0.0.1 auth.example.com
127.0.0.1 console.example.com
127.0.0.1 gateway.example.com
```

For multiple AI providers, add all required domains:

```text
127.0.0.1 api.example.com
127.0.0.1 auth.example.com

127.0.0.1 api.another-example.com
127.0.0.1 auth.another-example.com
```

**Important:**

- Keep existing Hosts entries.
- Do not delete the `localhost` entries.
- Do not replace the entire file with the example configuration.
- Add only the domains that need to be redirected through AISwitch.
- Do not use wildcard domain entries.

A typical Linux Hosts file may contain entries such as:

```text
127.0.0.1 localhost
127.0.1.1 my-linux-host

# AISwitch - AI Service Domains
127.0.0.1 api.example.com
127.0.0.1 auth.example.com
```

Your existing hostname and localhost entries should be preserved.

### Step 5: Save the Hosts File

If you are using Nano:

1. Press `Ctrl + O` to write the changes.
2. Press `Enter` to confirm the filename.
3. Press `Ctrl + X` to exit.

If you are using Vim:

1. Press `Esc`.
2. Type `:wq`.
3. Press `Enter`.

### Step 6: Refresh the DNS Cache

On systems using `systemd-resolved`:

```bash
sudo resolvectl flush-caches
```

You can check whether the service is running:

```bash
systemctl is-active systemd-resolved
```

If the command reports that the service is not active or does not exist, your distribution may use another DNS caching service or no local DNS cache at all.

For systems using `nscd`, you can restart its cache service:

```bash
sudo systemctl restart nscd
```

For systems using `dnsmasq`:

```bash
sudo systemctl restart dnsmasq
```

Only execute the restart command for a service that is actually installed and running on your system.

### Step 7: Verify the Hosts Configuration

Use:

```bash
getent hosts api.example.com
```

Expected output:

```text
127.0.0.1 api.example.com
```

You can also use:

```bash
ping -c 1 api.example.com
```

The output should indicate:

```text
PING api.example.com (127.0.0.1)
```

If `getent` returns the original public IP address:

1. Check the spelling of the hostname.
2. Check that the entry is not commented out with `#`.
3. Check that the entry points to `127.0.0.1`.
4. Verify that `/etc/hosts` is being used by the system name service configuration.
5. Check whether the application uses its own DNS resolver.

### Step 8: Restore the Original Hosts File

If you want to undo the changes:

```bash
sudo cp -a /etc/hosts.backup /etc/hosts
```

Verify the restored file:

```bash
cat /etc/hosts
```

If a local DNS cache is running, flush or restart the relevant cache service again.

## 8. macOS Configuration

Applicable to:

- macOS Intel
- macOS Apple Silicon
- macOS Sonoma
- macOS Sequoia
- Other supported macOS versions

The macOS Hosts file is located at:

```text
/etc/hosts
```

**Administrator privileges are required to modify this file.**

### Step 1: Open Terminal

Open:

```text
Applications
    -> Utilities
        -> Terminal
```

Alternatively, press:

```text
Command + Space
```

Search for:

```text
Terminal
```

Press Enter to open it.

### Step 2: Back Up the Original Hosts File

Before modifying the Hosts file, create a backup:

```bash
sudo cp -p /etc/hosts /etc/hosts.backup
```

Verify the backup:

```bash
ls -l /etc/hosts /etc/hosts.backup
```

The original file is preserved at:

```text
/etc/hosts.backup
```

### Step 3: Open the Hosts File

Execute:

```bash
sudo nano /etc/hosts
```

macOS will ask for your administrator password.

Enter your Mac login password and press Enter.

**Note:** The terminal does not display characters while you type your password. This is normal.

### Step 4: Add AI Service Domains

Move to the bottom of the file and append:

```text
# ==================================================
# AISwitch - AI Service Domains
# ==================================================

127.0.0.1 api.example.com
127.0.0.1 auth.example.com
127.0.0.1 console.example.com
127.0.0.1 gateway.example.com
```

You can add additional domains as needed:

```text
127.0.0.1 api.example.com
127.0.0.1 auth.example.com

127.0.0.1 api.another-example.com
127.0.0.1 auth.another-example.com
```

**Important:**

- Keep all existing entries.
- Do not remove the `localhost` entries.
- Add the new entries at the bottom.
- Do not use wildcard entries such as `*.example.com`.
- Only redirect the AI service domains that should pass through AISwitch.

### Step 5: Save the Hosts File

If you are using Nano:

1. Press `Control + O` to save.
2. Press `Enter` to confirm.
3. Press `Control + X` to exit.

Do not use `Command` instead of `Control` for Nano's save and exit shortcuts.

### Step 6: Flush the macOS DNS Cache

Execute the following commands:

```bash
sudo dscacheutil -flushcache
sudo killall -HUP mDNSResponder
```

These commands refresh the macOS DNS cache and notify the DNS responder.

No output is normally displayed when the commands succeed.

### Step 7: Verify the Hosts Configuration

Execute:

```bash
dscacheutil -q host -a name api.example.com
```

Expected output should include:

```text
ip_address: 127.0.0.1
```

You can also use:

```bash
ping -c 1 api.example.com
```

Expected output:

```text
PING api.example.com (127.0.0.1)
```

If the domain still resolves to its original public IP address:

1. Check the spelling of the hostname.
2. Check the `/etc/hosts` entry.
3. Make sure the entry is not commented out.
4. Flush the DNS cache again.
5. Check whether the application uses its own DNS resolver.

### Step 8: Restore the Original Hosts File

To restore the original configuration:

```bash
sudo cp -p /etc/hosts.backup /etc/hosts
```

Then refresh the DNS cache:

```bash
sudo dscacheutil -flushcache
sudo killall -HUP mDNSResponder
```

## 9. Configure and Start AISwitch

After completing the Hosts configuration for your operating system, start AISwitch.

### Option A: SOCKS5 Proxy

Windows:

```powershell
.\AISwitch.exe `
  -listen :443 `
  -target example.com:443 `
  -proxy-type socks5 `
  -socks5 192.168.1.100:1080
```

Linux / macOS:

```bash
sudo ./AISwitch \
  -listen :443 \
  -target example.com:443 \
  -proxy-type socks5 \
  -socks5 192.168.1.100:1080
```

### Option B: HTTP CONNECT Proxy

Windows:

```powershell
.\AISwitch.exe `
  -listen :443 `
  -target example.com:443 `
  -proxy-type http `
  -http-proxy 192.168.1.100:7890
```

Linux / macOS:

```bash
sudo ./AISwitch \
  -listen :443 \
  -target example.com:443 \
  -proxy-type http \
  -http-proxy 192.168.1.100:7890
```

Replace the example proxy address with your actual remote proxy address.

## 10. Verify the Configuration

After editing the Hosts file, verify that the domain resolves to the local address.

### Windows

```cmd
ping api.example.com
```

### Linux

```bash
getent hosts api.example.com
```

### macOS

```bash
dscacheutil -q host -a name api.example.com
```

The expected address is:

```text
127.0.0.1
```

Next, start AISwitch and check its connection logs.

When an application connects successfully, you should see information similar to:

```text
[CONN] CONNECT client=127.0.0.1 target=example.com:443 proxy=socks5

[SNI] Detected hostname=api.example.com

[SOCKS5] CONNECT proxy=192.168.1.100:1080 target=api.example.com:443

[CONN] ESTABLISHED target=api.example.com:443
```

The exact log format depends on the AISwitch version.

## 11. How SNI Routing Works

When an HTTPS client establishes a TLS connection, it normally sends a ClientHello message containing the destination hostname in the SNI extension.

For example:

```text
Client requests:
https://api.example.com/

TLS ClientHello:
SNI = api.example.com
```

AISwitch extracts the hostname and establishes the remote tunnel:

```text
CONNECT api.example.com:443
```

The remote proxy connects to the actual destination.

AISwitch then forwards the original TLS data without decrypting or modifying it.

This means:

- Original HTTPS encryption is preserved.
- The destination server handles the original TLS handshake.
- No local TLS certificate installation is required.
- Multiple HTTPS domains can share the same local listener.

If SNI is unavailable or cannot be parsed, AISwitch falls back to the configured `-target`.

## 12. Troubleshooting

### Port 443 Is Already in Use

Check whether another application is listening on port 443.

Windows:

```cmd
netstat -ano | findstr :443
```

Linux:

```bash
sudo ss -lntp | grep :443
```

macOS:

```bash
sudo lsof -iTCP:443 -sTCP:LISTEN
```

Stop the conflicting service before starting AISwitch.

### Connection Refused

Check:

1. Is AISwitch running?
2. Is it listening on port 443?
3. Does the Hosts file point to `127.0.0.1`?
4. Is another application occupying port 443?

### Remote Proxy Connection Failed

Check:

1. Is the remote proxy reachable?
2. Is the proxy address and port correct?
3. Does the HTTP proxy support HTTPS CONNECT?
4. Does the proxy allow connections to the requested destination?

### Some AI Service Domains Still Cannot Connect

Some AI providers use multiple domains for API requests, authentication, streaming, and other services.

Make sure all required hostnames are included in the Hosts file.

Also check whether the application uses its own DNS resolver, DNS-over-HTTPS, an IPv6 address, or a non-standard HTTPS port.

## 13. Limitations and Security Notes

- AISwitch currently focuses on HTTPS traffic over TCP, normally port 443.
- The current implementation does not provide general UDP forwarding or HTTP port 80 interception.
- TLS Encrypted ClientHello (ECH) may hide the actual SNI hostname.
- Connections without a usable SNI fall back to the configured default target.
- Only redirect the domains you intend to proxy.
- Keep the AISwitch listener bound to localhost or otherwise restrict access with firewall rules. Do not expose an unrestricted forwarding proxy to an untrusted network.
- Protect proxy credentials and avoid sharing command lines containing passwords.

## 14. Project Goal

AISwitch is designed to make AI service connectivity simple.

You do not need to modify your AI applications or configure individual proxy settings.

You only need:

1. A working HTTP CONNECT or SOCKS5 proxy.
2. AISwitch running locally.
3. The relevant AI service domains mapped to `127.0.0.1` in your Hosts file.

**Let AISwitch handle the routing. Keep your AI applications unchanged.**
