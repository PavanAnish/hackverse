$ErrorActionPreference = 'Stop'

function Get-Status($Uri) {
    try {
        $r = Invoke-WebRequest -Method Get -Uri $Uri
        return $r.StatusCode
    } catch {
        return $_.Exception.Response.StatusCode.value__
    }
}

function Options-Req($Uri) {
    try {
        $r = Invoke-WebRequest -Method Options -Uri $Uri -Headers @{ "Origin" = "http://localhost:3002"; "Access-Control-Request-Method" = "POST" }
        return $r
    } catch {
        return $_.Exception.Response
    }
}

Write-Host "Test B(a): Old route"
Invoke-RestMethod -Method Post -Uri "http://localhost:8002/chaos/fail?enable=true&type=default" | Out-Null
$status = Get-Status "http://localhost:8002/pay"
if ($status -ne 500) { throw "Expected 500 from old route fail, got $status" }
Invoke-RestMethod -Method Post -Uri "http://localhost:8002/chaos/fail?enable=false" | Out-Null
$status = Get-Status "http://localhost:8002/pay"
if ($status -ne 200) { throw "Expected 200 from old route recover, got $status" }
Write-Host "B(a) PASS"

Write-Host "Test B(b): New token route"
$body = @{ enable = $true; type = "default" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "http://localhost:8002/chaos/token" -Body $body -ContentType "application/json" | Out-Null
$status = Get-Status "http://localhost:8002/pay"
if ($status -ne 500) { throw "Expected 500 from new route fail, got $status" }
$body = @{ enable = $false; type = "default" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "http://localhost:8002/chaos/token" -Body $body -ContentType "application/json" | Out-Null
$status = Get-Status "http://localhost:8002/pay"
if ($status -ne 200) { throw "Expected 200 from new route recover, got $status" }
Write-Host "B(b) PASS"

Write-Host "Test B(c): CORS preflight with failure ON"
$body = @{ enable = $true; type = "default" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "http://localhost:8002/chaos/token" -Body $body -ContentType "application/json" | Out-Null

$res = Options-Req "http://localhost:8002/chaos/token"
if ($res.StatusCode -ne 200) { throw "Expected 200 for OPTIONS, got $($res.StatusCode)" }
if (-not $res.Headers.ContainsKey("Access-Control-Allow-Origin")) { throw "Missing CORS headers" }

$body = @{ enable = $false; type = "default" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "http://localhost:8002/chaos/token" -Body $body -ContentType "application/json" | Out-Null
Write-Host "B(c) PASS"
