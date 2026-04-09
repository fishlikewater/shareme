$ErrorActionPreference = "Stop"

Push-Location backend
go test ./...
Pop-Location

npm --prefix frontend test
