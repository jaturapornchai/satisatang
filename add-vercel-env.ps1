# Script to add environment variables to Vercel
# This script reads .env file and adds variables to Vercel

$envFile = ".env"

if (-not (Test-Path $envFile)) {
    Write-Host "Error: .env file not found" -ForegroundColor Red
    exit 1
}

Write-Host "Reading environment variables from .env..." -ForegroundColor Cyan

# Read .env file and parse variables
$envVars = @{}
Get-Content $envFile | ForEach-Object {
    $line = $_.Trim()
    # Skip empty lines and comments
    if ($line -and -not $line.StartsWith("#")) {
        $parts = $line -split "=", 2
        if ($parts.Count -eq 2) {
            $key = $parts[0].Trim()
            $value = $parts[1].Trim()
            # Remove quotes if present
            $value = $value -replace '^"', '' -replace '"$', ''
            $envVars[$key] = $value
        }
    }
}

Write-Host "Found $($envVars.Count) environment variables" -ForegroundColor Green

# Variables to add to Vercel
$requiredVars = @(
    "LINE_CHANNEL_SECRET",
    "LINE_CHANNEL_ACCESS_TOKEN",
    "GEMINI_API_KEY",
    "GEMINI_MODEL",
    "MONGODB_ATLAS_URI",
    "MONGODB_ATLAS_DBNAME",
    "FIREBASE_CREDENTIALS",
    "FIREBASE_STORAGE_BUCKET"
)

foreach ($varName in $requiredVars) {
    if ($envVars.ContainsKey($varName) -and $envVars[$varName]) {
        Write-Host "`nAdding $varName to Vercel..." -ForegroundColor Yellow
        
        # Use Vercel CLI to add environment variable
        # Note: This will prompt for environment selection (production, preview, development)
        $value = $envVars[$varName]
        
        # Echo the value to vercel env add command
        # We'll add to all environments (production, preview, development)
        Write-Output $value | vercel env add $varName production preview development
        
        if ($LASTEXITCODE -eq 0) {
            Write-Host "✓ Added $varName" -ForegroundColor Green
        } else {
            Write-Host "✗ Failed to add $varName" -ForegroundColor Red
        }
    } else {
        Write-Host "⊘ Skipping $varName (not found in .env)" -ForegroundColor Gray
    }
}

Write-Host "`n✅ Environment variables setup complete!" -ForegroundColor Green
Write-Host "Run 'vercel --prod' to redeploy with new environment variables" -ForegroundColor Cyan
