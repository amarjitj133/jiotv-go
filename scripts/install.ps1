try {
    # Prompt the user for permission to add the application to the system's PATH.
    $accessFromAnywhere = $null
    while ($accessFromAnywhere -notin @("yes", "no")) {
        $accessFromAnywhere = Read-Host "Do you want to be able to run 'jiotv_go' from any terminal? (This will add it to your system PATH) [yes/no]"
        if ($accessFromAnywhere -notin @("yes", "no")) {
            Write-Host "Invalid choice. Please enter 'yes' or 'no'."
        }
    }

    # Identify operating system architecture
    $architecture = (Get-WmiObject Win32_OperatingSystem).OSArchitecture
    switch ($architecture) {
        "64-bit" {
            $arch = "amd64"
            break
        }
        "32-bit" {
            $arch = "386"
            break
        }
        "ARM64" {
            $arch = "arm64"
            break
        }
        default {
            throw "Unsupported architecture: $architecture"
        }
    }

    Write-Host "Detected architecture: $arch"

    # Determine the user's home directory
    $homeDirectory = [System.IO.Path]::Combine($env:USERPROFILE, ".jiotv_go")

    # Create the directory if it doesn't exist
    if (-not (Test-Path $homeDirectory -PathType Container)) {
        New-Item -ItemType Directory -Force -Path $homeDirectory
    }

    # Change to the home directory
    Set-Location -Path $homeDirectory

    # If the binary already exists, delete it
    if (Test-Path jiotv_go.exe) {
        Write-Host "Deleting existing binary"
        Remove-Item jiotv_go.exe
    }

    # Fetch the latest binary
    $binaryUrl = "https://github.com/jiotv-go/jiotv_go/releases/latest/download/jiotv_go-windows-$arch.exe"
    Write-Host "Fetching the latest binary from $binaryUrl"
    Invoke-WebRequest -Uri $binaryUrl -OutFile jiotv_go.exe -UseBasicParsing

    if ($accessFromAnywhere -eq "yes") {
        # Check for admin privileges before modifying the system PATH
        $isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
        if (-not $isAdmin) {
            Write-Host "Adding to system PATH requires administrator privileges." -ForegroundColor Yellow
            Write-Host "Please re-run the script from a terminal with 'Run as Administrator'." -ForegroundColor Yellow
            throw "Administrator privileges required."
        }

        # Add the directory to PATH in the current session
        $env:Path = "$env:Path;$homeDirectory"
        
        # Modify system environment variable to persist
        [System.Environment]::SetEnvironmentVariable("Path", [System.Environment]::GetEnvironmentVariable("Path", [System.EnvironmentVariableTarget]::Machine) + ";$homeDirectory", [System.EnvironmentVariableTarget]::Machine)
        
        Write-Host "JioTV Go has successfully downloaded and added to PATH. Start by running jiotv_go help"
    } else {
        Write-Host "Remember this folder is $homeDirectory"
        Write-Host "JioTV Go has successfully downloaded. You can run it from the current folder. Start by running .\jiotv_go.exe help"
    }
}
catch {
    Write-Host "Error: $_"
}
