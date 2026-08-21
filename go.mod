module github.com/aronk11/havenry

go 1.22

require github.com/coder/websocket v1.8.12

require (
	github.com/goccy/go-yaml v1.15.13
	github.com/mattn/go-sqlite3 v1.14.50
	golang.org/x/crypto v0.0.0-00010101000000-000000000000
)

require (
	github.com/go-yaml/yaml v2.1.0+incompatible // indirect
	golang.org/x/sys v0.28.0 // indirect
)

replace golang.org/x/crypto => github.com/golang/crypto v0.31.0

replace golang.org/x/sys => github.com/golang/sys v0.28.0
