param(
    [string]$BaseUrl = "http://127.0.0.1:19100"
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# 读取本机代理的健康检查与启动快照。
$health = Invoke-RestMethod "$BaseUrl/api/health"
$bootstrap = Invoke-RestMethod "$BaseUrl/api/bootstrap"

Write-Host "健康检查响应："
$health | ConvertTo-Json -Depth 8

Write-Host "启动快照响应："
$bootstrap | ConvertTo-Json -Depth 8
