<##
.SYNOPSIS
    Allows Sage to connect to the Faire GUI fulfillment endpoint.

.DESCRIPTION
    Creates or replaces a narrowly scoped Windows Defender Firewall inbound rule
    for the Faire GUI's current Sage HTTP endpoint. The default values match the
    current application configuration: TCP port 18080 and Sage workstation
    address 192.168.128.10.

    Run this script on the Windows workstation that runs Faire GUI. The script
    only allows inbound traffic from the configured Sage workstation and only on
    Domain or Private network profiles.

.PARAMETER RemoteAddress
    IPv4 or IPv6 address of the workstation running Sage. The default matches the
    current repository configuration.

.PARAMETER LocalPort
    TCP port used by Faire GUI. The current plaintext endpoint uses port 18080.

.PARAMETER RuleName
    Display name assigned to the Windows Firewall rule.

.EXAMPLE
    .\Allow-FaireGuiSageFirewall.ps1

.EXAMPLE
    .\Allow-FaireGuiSageFirewall.ps1 -RemoteAddress "192.168.128.25"

.EXAMPLE
    .\Allow-FaireGuiSageFirewall.ps1 -LocalPort 443

.NOTES
    This script configures the firewall only; it does not start Faire GUI or
    enable the Sage integration. Run it from an elevated PowerShell session.
#>

#requires -Version 5.1

[CmdletBinding()]
param(
    [Parameter()]
    [ValidateNotNullOrEmpty()]
    [string] $RemoteAddress = "192.168.128.10",

    [Parameter()]
    [ValidateRange(1, 65535)]
    [int] $LocalPort = 18080,

    [Parameter()]
    [ValidateNotNullOrEmpty()]
    [string] $RuleName = "Faire GUI Sage Fulfillment"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Test-IsAdministrator {
    <#
    .SYNOPSIS
        Determines whether the current PowerShell process is elevated.

    .OUTPUTS
        System.Boolean
    #>
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Install-FaireGuiFirewallRule {
    <#
    .SYNOPSIS
        Creates the restricted inbound firewall rule for Sage.

    .PARAMETER DisplayName
        Display name for the firewall rule.

    .PARAMETER SourceAddress
        Address of the Sage workstation allowed to connect.

    .PARAMETER Port
        Local TCP port exposed by Faire GUI.

    .OUTPUTS
        Microsoft.Management.Infrastructure.CimInstance
    #>
    param(
        [Parameter(Mandatory)]
        [string] $DisplayName,

        [Parameter(Mandatory)]
        [string] $SourceAddress,

        [Parameter(Mandatory)]
        [int] $Port
    )

    $parsedAddress = $null
    if (-not [System.Net.IPAddress]::TryParse($SourceAddress, [ref] $parsedAddress)) {
        throw "RemoteAddress '$SourceAddress' is not a valid IP address."
    }

    # Replacing the named rule makes rerunning the script deterministic when the
    # Sage workstation address or endpoint port changes during deployment.
    Get-NetFirewallRule -DisplayName $DisplayName -ErrorAction SilentlyContinue |
        Remove-NetFirewallRule -ErrorAction Stop

    return New-NetFirewallRule `
        -DisplayName $DisplayName `
        -Direction Inbound `
        -Action Allow `
        -Protocol TCP `
        -LocalPort $Port `
        -RemoteAddress $SourceAddress `
        -Profile Domain,Private `
        -Description "Allows Sage workstation $SourceAddress to reach Faire GUI TCP port $Port."
}

if (-not (Test-IsAdministrator)) {
    throw "This script must be run as Administrator on the Faire GUI workstation."
}

$rule = Install-FaireGuiFirewallRule `
    -DisplayName $RuleName `
    -SourceAddress $RemoteAddress `
    -Port $LocalPort

Write-Host "Firewall rule configured successfully." -ForegroundColor Green
Write-Host "  Rule:           $RuleName"
Write-Host "  Direction:      Inbound"
Write-Host "  Protocol/port:  TCP/$LocalPort"
Write-Host "  Remote address: $RemoteAddress"
Write-Host "  Profiles:       Domain, Private"
Write-Host ""
Write-Host "Enable Sage integration in Faire GUI, then test from the Sage workstation with:"
Write-Host "  Test-NetConnection <faire-gui-hostname> -Port $LocalPort"
