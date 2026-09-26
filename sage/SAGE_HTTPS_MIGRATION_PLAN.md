# Sage transport HTTPS migration plan

## Purpose

Replace the temporary plaintext Sage-to-Faire GUI transport with HTTPS. This is a **hard cutover**: after release, Sage sends no requests to the prior HTTP listener.

This document is an implementation and Windows deployment plan only. It does not change the application yet.

## Confirmed decisions

| Item | Decision |
| --- | --- |
| Traffic in scope | All Sage fulfillment transport traffic: the fulfillment request and post-writeback acknowledgement. |
| Server | Faire GUI on the Windows GUI workstation (currently `RMT01`). |
| Client | Sage on the Windows Sage server/workstation (currently `BSDC01`, `192.168.128.10`). |
| Endpoint | HTTPS on TCP **443**. |
| Certificate | Self-signed, RSA certificate trusted explicitly by Sage clients. |
| Cutover | HTTPS only; no HTTP compatibility listener or redirect. |
| Client trust | Install the self-signed public certificate into the Sage machine's `LocalMachine\Root` store. |

## Current implementation inventory

The existing protocol is tightly coupled between these files:

| File | Current responsibility | Required change |
| --- | --- | --- |
| `sage/LaunchFaireFulfillment.vbs` | Posts and acknowledges over `http://RMT01:18080`. | Build `https://<configured-host>/...` URLs and retain normal Windows certificate validation. |
| `application/sage_fulfillment_http.go` | Opens `:18080`, owns the server and handler, and permits only `192.168.128.10`. | Load the TLS certificate, serve TLS on `:443`, and retain the request/peer/body/timeout controls. |
| `application/sage_fulfillment.go` | Starts/stops the opt-in listener and shows HTTP/18080 status text. | Use the HTTPS listener and revise all operator diagnostics. |
| `application/sage_fulfillment_test.go` | Covers payload decoding, request parsing, and peer rejection. | Retain protocol tests and add TLS/configuration integration coverage. |
| `PLAN.md` | Documents the temporary HTTP transport and operational checks. | Mark the old path as retired and reference this setup process. |

### Existing protocol that must not change

Only the transport changes. Keep these application-level contracts unchanged:

- `POST /v1/sage/fulfillment` carries UTF-8 JSON and returns the existing line-oriented terminal result.
- `POST /v1/sage/fulfillment/ack` carries the existing narrow acknowledgement JSON and returns `204 No Content`.
- The request ID remains the durable idempotency key.
- The 1 MiB request limit; 5-second header timeout; 15-second request-read timeout; 30-second idle timeout; and 11-minute response timeout remain in force.
- The GUI continues to accept requests only while the user has enabled the Sage integration.
- The handler continues to allow only the configured Sage peer address. TLS authenticates the GUI server; it does not replace the network allow-list.
- The UI goroutine and durable recovery flow remain unchanged.

## Target design

```text
BSDC01 (Sage / ServerXMLHTTP)
  └─ HTTPS, TLS 1.2+, validates RMT01 certificate
       └─ RMT01 (Faire GUI, opt-in TLS listener on :443)
            ├─ Windows Firewall: inbound TCP 443 from BSDC01's configured IP only
            ├─ TLS server certificate: DNS SAN matches the exact configured Sage URL host
            └─ existing handler: source allow-list, bounded JSON, response/replay/ack logic
```

### Certificate and identity policy

1. Use an RSA 3072-bit, SHA-256 self-signed certificate with the Server Authentication EKU.
2. Its DNS Subject Alternative Name (SAN) must include the **exact hostname used by Sage**. If Sage calls `https://RMT01/...`, include `RMT01`. If it calls an FQDN or alias, include that name too and use that exact name in the VBS configuration.
3. Do **not** use an IP address in the URL unless an IP SAN is deliberately added and validated. The standard deployment uses DNS names.
4. Trust only the public `.cer` on Sage clients. Keep the private-key `.pfx` on the GUI workstation and readable only by the account running Faire GUI plus `SYSTEM` and local Administrators.
5. Require TLS 1.2 or newer. Allow Go's secure default TLS 1.3 support; do not pin cipher suites unless a verified Sage/Windows compatibility issue requires it.
6. Do not bypass certificate errors in VBScript. In particular, do not set an MSXML option that ignores unknown CA, hostname, expiration, or revocation errors.
7. Retain the Windows Firewall and in-process source-IP allow-list. HTTPS alone encrypts and authenticates the GUI server; it does not cryptographically authenticate the Sage client. If client cryptographic identity becomes a requirement, scope a separate mutual-TLS design and test it with Sage's certificate-selection behavior.

### Configuration model

Avoid embedding site-specific host names, peer addresses, paths, or certificate locations in Go constants.

Introduce a small, validated `sageTLSConfig` with:

```text
listenerAddress: ":443"
allowedPeerAddress: "192.168.128.10"
serverName: "RMT01"
pfxPath: "C:\\ProgramData\\Faire GUI\\SageTLS\\server.pfx"
certificateThumbprint: "<uppercase SHA-1 certificate thumbprint>"
keyringService: "faire-gui"
keyringAccount: "sage-tls-pfx-password"
```

Store the non-secret configuration in the existing per-user Faire GUI settings location. Store the PFX password in the current GUI user's Windows Credential Manager through the existing `github.com/zalando/go-keyring` dependency. The password must never be written to JSON, source control, logs, status messages, or command-line arguments.

Add a small non-GUI bootstrap command to the executable:

```text
faire-gui.exe sage-tls configure \
  --pfx <path> \
  --server-name <DNS name> \
  --listen-address :443 \
  --allowed-peer <IPv4 or IPv6 address> \
  --password-stdin
```

The command should validate the PFX, verify that its SAN covers `--server-name`, save only non-secret values, and place the password received from standard input into Credential Manager. It should fail before saving if the certificate is expired, lacks Server Authentication, lacks the requested DNS SAN, or the private key cannot be loaded.

## Detailed implementation plan

### 1. Establish the deployment inputs before coding

For every site, record these values in a deployment worksheet:

| Value | Example | Why it matters |
| --- | --- | --- |
| GUI workstation DNS name | `RMT01` | Must match both the VBS URL and certificate SAN. |
| Optional FQDN/alias names | `RMT01.example.internal` | Add every name actually used by Sage to the certificate SAN. |
| GUI workstation IP | Site-specific | Required only if restrictive Sage outbound policy rules are used. |
| Sage client IP | `192.168.128.10` | Used by the Windows Firewall and Go peer allow-list. |
| GUI user account | `DOMAIN\\fairegui` | Receives read access to the PFX and owns the Credential Manager secret. |
| Network profile | `Domain` or `Private` | Firewall rule should not unnecessarily enable `Public`. |
| Certificate expiry/rotation owner | Named operator/team | Self-signed certificates do not renew automatically. |

Preflight checks:

1. Confirm forward DNS from the Sage computer resolves the chosen GUI name to the expected workstation.
2. Confirm the GUI workstation will not use a different name, CNAME, or raw IP in the Sage URL.
3. Confirm TCP 443 is not already owned on the GUI workstation. A Go TCP listener does **not** need an HTTP.sys URL reservation, but IIS, http.sys, a reverse proxy, or another process may already own the port.
4. Confirm the Sage machine's Windows/SChannel policy permits TLS 1.2 and that `MSXML2.ServerXMLHTTP.6.0` can negotiate it. This is a release gate, not an assumption.
5. Back up the deployed Sage Script Link script and record the current certificate thumbprint before changing a working site.

### 2. Refactor the Go transport around explicit TLS configuration

1. Move the hard-coded listener address and permitted peer address out of `application/sage_fulfillment_http.go` into a validated configuration loader.
2. Rename HTTP-specific transport symbols to transport-neutral or HTTPS names, for example:
   - `serveSageFulfillmentHTTP` → `serveSageFulfillmentHTTPS`
   - `sageFulfillmentHTTPAddress` → configuration-backed listener address
   - `sageFulfillmentHTTPAllowedHost` → configuration-backed allowed peer
3. Keep `net/http` and the handler structure; HTTPS is HTTP carried inside TLS. Do not rewrite the fulfillment protocol or the UI message flow.
4. Load the PFX into a `tls.Certificate`. Add the minimal `golang.org/x/crypto/pkcs12` dependency if necessary; use the certificate chain supplied by the PFX.
5. Create `tls.Config` with:
   - the loaded server certificate;
   - `MinVersion: tls.VersionTLS12`;
   - no `InsecureSkipVerify` equivalent and no custom cipher-suite downgrade.
6. Start the existing `http.Server` through a TLS listener or `ServeTLS` on `:443`. Preserve graceful shutdown and all current server timeouts.
7. Fail closed: if TLS configuration, the PFX, its password, certificate validity, or port bind fails, do not enable the listener. Display a precise non-secret operator message and leave the persisted Sage integration setting disabled.
8. Validate the peer address with `net/netip` rather than string comparison where practical. Normalize the configured address and accept the equivalent representation returned by `RemoteAddr`; reject all other peers. Do not trust `X-Forwarded-For`.
9. Continue to set only the existing `text/plain; charset=utf-8` response content type and keep the existing body-size and payload-decoding behavior.
10. Update `startSageFulfillmentListener`, listener failure handling, Settings copy, comments, and error strings so they reference HTTPS and TCP 443, never the retired HTTP/18080 endpoint.

### 3. Update the Sage Script Link transport

In `sage/LaunchFaireFulfillment.vbs`:

1. Replace the transport constants with a configured HTTPS host and port 443. Prefer the standard URL without `:443`:

   ```vbscript
   Const FAIRE_GUI_HTTPS_HOST = "RMT01"
   Const FAIRE_HTTPS_PORT = 443

   Function GetFulfillmentURL()
       GetFulfillmentURL = "https://" & FAIRE_GUI_HTTPS_HOST & FAIRE_HTTP_PATH
   End Function

   Function GetFulfillmentAckURL()
       GetFulfillmentAckURL = "https://" & FAIRE_GUI_HTTPS_HOST & FAIRE_HTTP_ACK_PATH
   End Function
   ```

   Rename the remaining `FAIRE_HTTP_*` names to `FAIRE_HTTPS_*` during the code change so comments and diagnostics remain accurate.

2. Keep `MSXML2.ServerXMLHTTP.6.0`, its asynchronous request flow, UTF-8 byte encoding, request headers, status handling, and timeouts. They are protocol behaviors independent of TLS.
3. Do not add an HTTP fallback, redirect following workaround, hostname override, or certificate-validation bypass. A certificate error must fail the action before Sage writeback, with an actionable message identifying the configured GUI host.
4. Update operator-facing failure text from “temporary HTTP endpoint” to “HTTPS endpoint” without leaking certificate passwords or customer data.
5. Keep both request and acknowledgement URLs on the same HTTPS origin and retain their existing paths and status expectations.

### 4. Add focused automated coverage

Add only behavior-level tests that would expose a broken migration:

| Test | Observable assertion |
| --- | --- |
| Valid TLS configuration | A test certificate with a matching DNS SAN and valid key starts the listener; a TLS 1.2+ client can POST from the permitted peer and receives the existing response contract. |
| Invalid certificate configuration | Missing PFX, wrong password, expired certificate, or SAN mismatch fails startup without opening a listener. |
| TLS minimum version | A client restricted below TLS 1.2 cannot negotiate; TLS 1.2 (and supported TLS 1.3) succeeds. |
| Peer restriction | A valid TLS request from a non-permitted peer gets `403`; the configured peer proceeds to existing parsing logic. |
| Protocol compatibility | Existing parsing, result formatting, acknowledgement, replay, request-size, cancellation, and timeout tests remain unchanged and pass over TLS. |
| No plaintext listener | The application no longer binds TCP 18080, and a plaintext HTTP request to 443 cannot receive a valid HTTP protocol response. |
| Bootstrap configuration | The configure command rejects malformed/non-SAN-matching material and saves no secret to the settings file. |

Use generated test certificates and loopback test listeners. Do not depend on a real Windows certificate store, Sage installation, network, or wall-clock certificate expiry in unit tests. Keep Windows/Sage verification as a manual release gate.

### 5. Update operational documentation

Before release, update `PLAN.md`, the relevant README/setup documentation, code comments, and all deployment instructions in the same change. Remove statements that TCP 18080 or plaintext HTTP is the active transport. Document:

- the two HTTPS paths;
- the exact DNS/SAN matching rule;
- the TCP 443 firewall scope;
- the certificate-installation and rotation procedures below;
- the absence of an HTTP fallback; and
- the supported recovery and rollback procedure.

## Windows deployment scripts

The following scripts are the proposed files for `scripts/windows/`. They are included here so a future implementation can create them verbatim, review them, and use them consistently on each site.

### `New-FaireGuiSageTlsCertificate.ps1` — GUI workstation bootstrap

Run this **as Administrator on the Faire GUI workstation**. Run it before starting the new HTTPS-enabled Faire GUI. The GUI user must be the Windows account that will launch Faire GUI.

```powershell
#Requires -RunAsAdministrator
<#
.SYNOPSIS
Creates a self-signed TLS certificate, exports the GUI server PFX and public CER,
and opens a narrowly scoped inbound Windows Firewall rule.

.PARAMETER GuiHostName
The DNS name Sage will use in the HTTPS URL. It must not be an IP address.

.PARAMETER AdditionalDnsNames
Optional FQDNs or aliases that Sage may use; each is added as a DNS SAN.

.PARAMETER AllowedSageAddress
The sole Sage client IP address permitted to connect to TCP 443.

.PARAMETER GuiUser
The account that runs Faire GUI. It receives read access to the PFX directory.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateNotNullOrEmpty()]
    [string]$GuiHostName,

    [string[]]$AdditionalDnsNames = @(),

    [Parameter(Mandatory)]
    [ValidateNotNullOrEmpty()]
    [string]$AllowedSageAddress,

    [Parameter(Mandatory)]
    [ValidateNotNullOrEmpty()]
    [string]$GuiUser,

    [ValidateRange(1, 5)]
    [int]$ValidYears = 2,

    [string]$InstallRoot = "$env:ProgramData\Faire GUI\SageTLS"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$parsedAddress = [System.Net.IPAddress]::None
if (-not [System.Net.IPAddress]::TryParse($AllowedSageAddress, [ref]$parsedAddress)) {
    throw "AllowedSageAddress '$AllowedSageAddress' is not a valid IPv4 or IPv6 address."
}

$parsedHostAddress = [System.Net.IPAddress]::None
if ([System.Net.IPAddress]::TryParse($GuiHostName, [ref]$parsedHostAddress)) {
    throw 'GuiHostName must be a DNS name because the certificate and Sage URL must use matching DNS SANs.'
}

$dnsNames = @($GuiHostName) + $AdditionalDnsNames | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique
if ($dnsNames.Count -eq 0) {
    throw 'At least one DNS SAN is required.'
}

$existingListener = Get-NetTCPConnection -State Listen -LocalPort 443 -ErrorAction SilentlyContinue
if ($null -ne $existingListener) {
    throw 'TCP port 443 is already listening. Identify and stop or reconfigure its owner before installing Faire GUI HTTPS.'
}

New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null
& icacls.exe $InstallRoot '/inheritance:r' '/grant:r' 'SYSTEM:(OI)(CI)F' 'BUILTIN\Administrators:(OI)(CI)F' "$GuiUser:(OI)(CI)RX" | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "Unable to apply the required ACL to '$InstallRoot'."
}

# RSA maximizes compatibility with the older Windows/SChannel client used by Sage.
$certificate = New-SelfSignedCertificate `
    -Type SSLServerAuthentication `
    -Subject "CN=$GuiHostName" `
    -DnsName $dnsNames `
    -CertStoreLocation 'Cert:\LocalMachine\My' `
    -KeyAlgorithm RSA `
    -KeyLength 3072 `
    -HashAlgorithm SHA256 `
    -KeyExportPolicy Exportable `
    -NotAfter (Get-Date).AddYears($ValidYears)

$pfxPath = Join-Path $InstallRoot 'server.pfx'
$cerPath = Join-Path $InstallRoot 'server.cer'
$pfxPassword = Read-Host -Prompt 'Enter a new PFX password; record it securely for the GUI-user configuration step' -AsSecureString
if ($pfxPassword.Length -eq 0) {
    throw 'The PFX password must not be empty.'
}

Export-PfxCertificate -Cert "Cert:\LocalMachine\My\$($certificate.Thumbprint)" -FilePath $pfxPath -Password $pfxPassword -ChainOption EndEntityCertOnly | Out-Null
Export-Certificate -Cert "Cert:\LocalMachine\My\$($certificate.Thumbprint)" -FilePath $cerPath -Type CERT | Out-Null

$ruleName = 'Faire GUI Sage HTTPS (TCP 443 from configured Sage host)'
Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue | Remove-NetFirewallRule
New-NetFirewallRule `
    -DisplayName $ruleName `
    -Direction Inbound `
    -Action Allow `
    -Protocol TCP `
    -LocalPort 443 `
    -RemoteAddress $AllowedSageAddress `
    -Profile Domain,Private | Out-Null

Write-Host "Public certificate: $cerPath"
Write-Host "PFX (private key): $pfxPath"
Write-Host "Certificate thumbprint: $($certificate.Thumbprint)"
Write-Host "Expires: $($certificate.NotAfter.ToString('u'))"
Write-Host 'Copy only the public .cer to Sage clients. Verify the displayed thumbprint out of band before trusting it.'
```

Example:

```powershell
.\New-FaireGuiSageTlsCertificate.ps1 `
  -GuiHostName 'RMT01' `
  -AdditionalDnsNames @('RMT01.example.internal') `
  -AllowedSageAddress '192.168.128.10' `
  -GuiUser 'DOMAIN\fairegui'
```

### `Configure-FaireGuiSageTlsUser.ps1` — Faire GUI user configuration

Run this **while signed in as the actual Faire GUI user**, not as an elevated deployment account. It invokes the planned bootstrap command so the PFX password is saved in that user's Windows Credential Manager rather than in a file.

```powershell
<#
.SYNOPSIS
Registers an already-provisioned Faire GUI TLS PFX for the current Faire GUI user.

.NOTES
This script requires the future `faire-gui.exe sage-tls configure` command described
in this plan. The PFX password is supplied over standard input, not as an argument.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateScript({ Test-Path $_ -PathType Leaf })]
    [string]$FaireGuiExe,

    [Parameter(Mandatory)]
    [ValidateScript({ Test-Path $_ -PathType Leaf })]
    [string]$PfxPath,

    [Parameter(Mandatory)]
    [ValidateNotNullOrEmpty()]
    [string]$ServerName,

    [Parameter(Mandatory)]
    [ValidateNotNullOrEmpty()]
    [string]$AllowedSageAddress
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$securePassword = Read-Host -Prompt 'Enter the PFX password created on the GUI workstation' -AsSecureString
$passwordBstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($securePassword)
try {
    $plainPassword = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($passwordBstr)
    # The planned command trims only the terminating newline added by this pipeline.
    $plainPassword | & $FaireGuiExe sage-tls configure `
        --pfx $PfxPath `
        --server-name $ServerName `
        --listen-address ':443' `
        --allowed-peer $AllowedSageAddress `
        --password-stdin
    if ($LASTEXITCODE -ne 0) {
        throw "Faire GUI TLS configuration failed with exit code $LASTEXITCODE."
    }
}
finally {
    if ($passwordBstr -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordBstr)
    }
    Remove-Variable plainPassword -ErrorAction SilentlyContinue
}
```

Example:

```powershell
.\Configure-FaireGuiSageTlsUser.ps1 `
  -FaireGuiExe 'C:\Program Files\Faire GUI\faire-gui.exe' `
  -PfxPath 'C:\ProgramData\Faire GUI\SageTLS\server.pfx' `
  -ServerName 'RMT01' `
  -AllowedSageAddress '192.168.128.10'
```

### `Install-FaireGuiSageTlsTrust.ps1` — Sage client trust bootstrap

Run this **as Administrator on every Sage client**. Transfer only `server.cer`, and compare the displayed server thumbprint through a separate trusted channel before executing the script.

```powershell
#Requires -RunAsAdministrator
<#
.SYNOPSIS
Verifies and trusts the Faire GUI self-signed public certificate for all local users.

.PARAMETER ExpectedThumbprint
The certificate thumbprint shown by the GUI-workstation bootstrap script. It is an
out-of-band verification value, not a secret.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateScript({ Test-Path $_ -PathType Leaf })]
    [string]$CertificatePath,

    [Parameter(Mandatory)]
    [ValidatePattern('^[0-9A-Fa-f ]+$')]
    [string]$ExpectedThumbprint,

    # Set this only where outbound firewall policy is default-deny.
    [switch]$AddOutboundRule,

    [string]$GuiWorkstationAddress
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$certificate = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2($CertificatePath)
$actualThumbprint = $certificate.Thumbprint.Replace(' ', '').ToUpperInvariant()
$expected = $ExpectedThumbprint.Replace(' ', '').ToUpperInvariant()
if ($actualThumbprint -ne $expected) {
    throw "Certificate thumbprint mismatch. Expected $expected; received $actualThumbprint."
}
if ($certificate.NotAfter -le (Get-Date)) {
    throw "Certificate expired at $($certificate.NotAfter.ToString('u'))."
}

Import-Certificate -FilePath $CertificatePath -CertStoreLocation 'Cert:\LocalMachine\Root' | Out-Null

if ($AddOutboundRule) {
    if ([string]::IsNullOrWhiteSpace($GuiWorkstationAddress)) {
        throw 'GuiWorkstationAddress is required when AddOutboundRule is specified.'
    }
    $ruleName = 'Sage to Faire GUI HTTPS (TCP 443)'
    Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    New-NetFirewallRule `
        -DisplayName $ruleName `
        -Direction Outbound `
        -Action Allow `
        -Protocol TCP `
        -RemoteAddress $GuiWorkstationAddress `
        -RemotePort 443 `
        -Profile Domain,Private | Out-Null
}

Write-Host "Trusted Faire GUI certificate $actualThumbprint in LocalMachine\Root."
```

Example for normal outbound-allow policy:

```powershell
.\Install-FaireGuiSageTlsTrust.ps1 `
  -CertificatePath 'C:\Temp\RMT01-server.cer' `
  -ExpectedThumbprint 'PASTE_THE_VERIFIED_THUMBPRINT_HERE'
```

### `Remove-FaireGuiSageHttpsFirewallRule.ps1` — emergency network rollback

This removes only the inbound firewall allowance. It does not delete the certificate or change Sage. Use it to immediately stop new Sage-to-GUI connections while preserving recovery data.

```powershell
#Requires -RunAsAdministrator
$ErrorActionPreference = 'Stop'
$ruleName = 'Faire GUI Sage HTTPS (TCP 443 from configured Sage host)'
Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue | Remove-NetFirewallRule
Write-Host 'Removed the Faire GUI Sage HTTPS inbound firewall rule.'
```

### Port and connectivity checks

Run these in addition to the scripts:

```powershell
# On RMT01, before enabling the GUI integration: 443 must have no listener.
Get-NetTCPConnection -State Listen -LocalPort 443 -ErrorAction SilentlyContinue

# On BSDC01, after the GUI listener is enabled: verifies DNS, route, and TCP firewall path.
Test-NetConnection -ComputerName RMT01 -Port 443 -InformationLevel Detailed

# On RMT01: confirms the narrowly scoped inbound rule.
Get-NetFirewallRule -DisplayName 'Faire GUI Sage HTTPS (TCP 443 from configured Sage host)' |
  Get-NetFirewallAddressFilter |
  Format-List RemoteAddress
```

`Test-NetConnection` verifies TCP reachability, not a successful TLS request. The controlled Sage transaction below is the final TLS/SChannel validation.

## Deployment and cutover runbook

1. **Prepare:** Complete the deployment worksheet and back up the current VBS Script Link implementation. Do not modify the production Sage button yet.
2. **Provision RMT01:** Run `New-FaireGuiSageTlsCertificate.ps1` as Administrator. Record the thumbprint and expiry. Do not distribute the PFX.
3. **Configure GUI user:** Sign in as the account that runs Faire GUI and run `Configure-FaireGuiSageTlsUser.ps1` using the same PFX password.
4. **Trust BSDC01:** Copy only `server.cer` to BSDC01 through an approved channel. Verify its thumbprint out of band, then run `Install-FaireGuiSageTlsTrust.ps1` as Administrator.
5. **Build and install:** Install a release containing the HTTPS-only listener, settings/configuration support, and updated Sage VBS. Do not start an HTTP listener at any point.
6. **Verify port ownership:** Before enabling Sage integration, verify TCP 443 is free; then start Faire GUI and enable the Sage integration. Verify it binds TCP 443 and the narrow firewall rule still limits the remote address to BSDC01.
7. **Update Sage Script Link:** Deploy the VBS with the exact HTTPS DNS host. Reconfirm that the host string exactly matches a certificate SAN. Do not use the old `http://RMT01:18080` address.
8. **Controlled functional test:** From BSDC01, run a test shipment that exercises a normal request and acknowledgement. Verify the GUI receives it, the expected operator review works, the typed result is applied once, and `APPLIED` clears the recovery entry.
9. **Negative test:** From an unauthorized host, verify a connection/request is blocked by the RMT01 firewall and, if the firewall is temporarily opened for test purposes, receives `403` from the peer allow-list. Restore the narrow firewall rule immediately.
10. **Failure-path test:** Temporarily remove the trusted public certificate from a non-production Sage test client. Verify the VBS fails before Sage writeback and no certificate-validation bypass has been added. Reinstall the certificate afterward.
11. **Cut over:** Remove the old TCP 18080 firewall rule and verify no process listens on 18080. The release must not provide a compatibility path.
12. **Record:** Store the deployment inputs, certificate thumbprint, expiry date, script version, installed Faire GUI version, and successful test date in the site runbook.

## Acceptance criteria

The migration is complete only when all of the following are true:

- [ ] The production GUI listener accepts only TLS 1.2+ on TCP 443 and does not bind TCP 18080.
- [ ] The server certificate is valid, has Server Authentication usage, and has a DNS SAN matching the actual Sage HTTPS host.
- [ ] BSDC01 trusts the exact verified public certificate in `LocalMachine\Root`.
- [ ] `MSXML2.ServerXMLHTTP.6.0` completes a validated HTTPS request without a certificate-validation override.
- [ ] RMT01's inbound firewall permits TCP 443 only from the configured Sage IP on Domain/Private profiles.
- [ ] The Go handler independently rejects any non-configured remote peer.
- [ ] Request, response, acknowledgement, replay, cancellation, recovery, size limits, and timeout behavior are unchanged.
- [ ] A duplicate request still replays the durable result rather than performing a second Faire action.
- [ ] All tests listed above and `go test ./...`, `go vet ./...`, and relevant race tests pass.
- [ ] `PLAN.md`, source comments, and operator instructions contain no stale active-reference to HTTP/18080.

## Certificate rotation and incident procedure

### Scheduled rotation

Begin rotation at least 30 days before expiry:

1. Generate a new self-signed certificate on RMT01 with the same required DNS SANs using the bootstrap script. Use a new PFX password.
2. Verify and install the new public `.cer` on every Sage client **before** deploying the new server PFX. Temporarily trusting both old and new roots avoids an outage.
3. Run the GUI-user configuration script with the new PFX and restart/re-enable the GUI listener during a maintenance window.
4. Run a controlled Sage request and acknowledgement.
5. After all sites use the new certificate, remove the old certificate from Sage clients' trusted root stores and archive the old deployment record.

### Suspected key compromise or lost workstation

1. Immediately run the emergency firewall-removal script on RMT01 or otherwise block TCP 443 from Sage.
2. Disable the Sage integration in Faire GUI; preserve the durable recovery data and document active request IDs.
3. Remove the compromised certificate from the server and every Sage client's trusted root store.
4. Generate and distribute a replacement certificate, configure the GUI user, and repeat the controlled test before reopening the firewall.
5. Review Windows access to the PFX directory and the account that runs Faire GUI. Because self-signed certificates have no central revocation service, trust-store removal is the revocation mechanism.

## Rollback

A rollback is an operational rollback, not a silent protocol downgrade:

1. Disable the Sage integration in Faire GUI and remove the TCP 443 firewall rule.
2. Preserve the GUI recovery data and record any unacknowledged request IDs; resolve those through the existing recovery flow before changing transports.
3. Restore the prior **known-good** release and backed-up Sage Script Link only if the business explicitly accepts temporary use of the prior controlled HTTP test path.
4. Restore its narrowly scoped TCP 18080 firewall rule only for that approved temporary rollback.
5. Open a follow-up task to remediate HTTPS; do not leave the HTTP path as an indefinite fallback.

## Risks and decisions to revisit

| Risk | Mitigation / decision |
| --- | --- |
| Port 443 conflict | Preflight and fail startup rather than sharing, proxying, or silently switching ports. |
| Hostname/SAN mismatch | Use one configured DNS host and validate SAN coverage in the bootstrap command before listener startup. |
| Untrusted self-signed certificate | Install the verified public cert on Sage clients; never disable validation. |
| Certificate expiry | Record owner/expiry and use the scheduled rotation procedure. |
| Client IP changes | Update the site worksheet, firewall rule, and application config together; test before production use. |
| PFX read/password exposure | Restrict NTFS access, store password in the GUI user's Credential Manager, and never distribute the PFX. |
| TLS does not authenticate Sage client | Retain firewall and peer allow-list; plan mTLS separately if client cryptographic authentication is required. |
| Deployment user differs from GUI user | Run the GUI-user configuration script in the actual GUI user's Windows session. |
| Existing active transport is still documented as temporary | Update all docs in the same release and remove old port rules after cutover. |
