run:
	GOOS=js GOARCH=wasm go build -o test.wasm  ./wasmmod/
	npm run build
	go run serve.go
