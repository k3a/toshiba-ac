INP := toshiba.go

build: toshiba-ac toshiba-ac-arm

toshiba-ac: $(INP)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $@
	#strip toshiba-ac

toshiba-ac-arm: $(INP)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -o $@
	#strip toshiba-ac
