module github.com/OpenMellon/openpdf

go 1.26.0

toolchain go1.27.3

require (
	github.com/OpenMellon/unitype v0.1.1
	github.com/adrg/sysfont v0.1.2
	github.com/h2non/filetype v1.1.3
	github.com/stretchr/testify v1.12.1
	github.com/unidoc/freetype v0.2.5
	github.com/unidoc/garabic v0.0.0-20220702200334-8c7cb25baa11
	github.com/unidoc/go-xmp v1.1.0
	github.com/unidoc/pkcs7 v0.4.2
	github.com/unidoc/timestamp v0.0.0-20200412005513-91597fd3793a
	github.com/unidoc/typesetting v0.1.1
	github.com/unidoc/unichart v0.5.2
	golang.org/x/crypto v0.58.0
	golang.org/x/image v0.47.0
	golang.org/x/net v0.61.0
	golang.org/x/sync v0.24.0
	golang.org/x/text v0.43.0
	golang.org/x/tools v0.52.0
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da
)

require (
	github.com/adrg/strutil v0.3.1 // indirect
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/sirupsen/logrus v1.10.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261009151841-97e8ea77aa2a // indirect
	mvdan.cc/gofumpt v0.12.0 // indirect
)

tool (
	golang.org/x/tools/cmd/goimports
	mvdan.cc/gofumpt
)
