# Windows client smoke test: the installed cs.exe talks to a real csbroker running on this same Windows machine.
# Used by .github/workflows/ci.yml (windows job); you can run it by hand too:
#   pwsh -File windows_client_smoke.ps1 -CsExe <path\cs.exe> -BrokerExe <path\csbroker.exe>
# Everything secret (admin password, join token) is generated per run and never printed.
# Config goes to a throw-away folder (CS_CONFIG_DIR), so your real cs config is not touched.
param(
  [Parameter(Mandatory)][string]$CsExe,
  [Parameter(Mandatory)][string]$BrokerExe,
  [string]$Work = (Join-Path ([IO.Path]::GetTempPath()) ("cs-win-smoke-" + [guid]::NewGuid().ToString('N').Substring(0, 8))),
  [int]$HttpPort = 18088,
  [int]$SshPort = 12222
)
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path "$Work\broker", "$Work\cfg" | Out-Null
$env:CS_CONFIG_DIR = "$Work\cfg"
$state = "$Work\broker"
$broker = $null
$fails = 0
function Check($ok, $msg) { if ($ok) { Write-Host "PASS $msg" } else { Write-Host "FAIL $msg"; $script:fails++ } }

try {
  # random admin password, set through stdin (never on the command line, never printed)
  $bytes = New-Object byte[] 12; [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
  $apw = 'Adm-' + (($bytes | ForEach-Object { $_.ToString('x2') }) -join '') + '-Xy'
  $apw | & $BrokerExe admin set-password --state $state *> $null
  Check ($LASTEXITCODE -eq 0) 'broker: admin password set'
  $tok = (& $BrokerExe token create --state $state 2>$null | Out-String).Trim()
  Check ($LASTEXITCODE -eq 0 -and $tok.Length -gt 5) 'broker: join token created (not printed)'

  $broker = Start-Process -FilePath $BrokerExe -PassThru -WindowStyle Hidden `
    -RedirectStandardOutput "$Work\broker.out.log" -RedirectStandardError "$Work\broker.err.log" `
    -ArgumentList @('serve', '--state', $state, '--config', 'NUL', '--http', "127.0.0.1:$HttpPort", '--ssh', "127.0.0.1:$SshPort",
      '--require-approval', 'true', '--panel', 'true', '--panel-secure-cookie', 'false')
  $up = $false
  for ($i = 0; $i -lt 50 -and -not $up; $i++) {
    try { $up = (Invoke-WebRequest -UseBasicParsing "http://127.0.0.1:$HttpPort/healthz").StatusCode -eq 200 } catch { Start-Sleep -Milliseconds 300 }
  }
  Check $up 'broker: csbroker started on Windows and /healthz answers'
  if (-not $up) { Get-Content "$Work\broker.err.log" -ErrorAction SilentlyContinue | Select-Object -Last 15; throw 'broker did not start' }

  & $CsExe init --server "127.0.0.1:$SshPort" --api "http://127.0.0.1:$HttpPort" *> $null
  Check ($LASTEXITCODE -eq 0 -and (Test-Path "$Work\cfg\config.json")) 'cs init: config saved'

  $json = (& $CsExe nodes --json | Out-String).Trim()
  $rc = $LASTEXITCODE
  $parsed = $true
  try { $null = $json | ConvertFrom-Json } catch { $parsed = $false }
  Check ($rc -eq 0 -and $parsed) "cs nodes --json: broker answered (rc=$rc, body=$json)"
  & $CsExe nodes *> $null
  Check ($LASTEXITCODE -eq 0) 'cs nodes: exit 0 against the running broker'

  # the proof that cs really talks to the broker: with the broker stopped the same command must fail
  Stop-Process -Id $broker.Id -Force; $broker = $null; Start-Sleep -Seconds 1
  & $CsExe nodes *> $null
  Check ($LASTEXITCODE -ne 0) 'cs nodes fails when the broker is stopped (so the PASS above was a real connection)'
}
catch { Write-Host "ERROR $($_.Exception.Message)"; $fails++ }
finally {
  if ($broker) { Stop-Process -Id $broker.Id -Force -ErrorAction SilentlyContinue }
  Remove-Item Env:\CS_CONFIG_DIR -ErrorAction SilentlyContinue
}
Write-Host "== $fails failed"
if ($fails -gt 0) { exit 1 }
