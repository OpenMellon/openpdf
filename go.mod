module github.com/OpenMellon/openpdf

go 1.27.3

require (
	github.com/adrg/sysfont v0.1.2
	github.com/go-text/typesetting-utils v0.0.0-20260327125527-fbf04b32d9ad
	github.com/golang/snappy v1.0.0
	github.com/h2non/filetype v1.1.3
	github.com/montanaflynn/stats v0.14.0
	github.com/sirupsen/logrus v1.9.4
	github.com/stretchr/testify v1.12.1
	golang.org/x/crypto v0.58.0
	golang.org/x/image v0.23.0
	golang.org/x/net v0.61.0
	golang.org/x/sync v0.24.0
	golang.org/x/text v0.43.0
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da
)

require (
	github.com/adrg/strutil v0.3.1 // indirect
	github.com/adrg/xdg v0.5.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261009151841-97e8ea77aa2a // indirect
	golang.org/x/tools v0.52.0 // indirect
	mvdan.cc/gofumpt v0.12.0 // indirect
)

tool (
	golang.org/x/tools/cmd/goimports
	mvdan.cc/gofumpt
)
