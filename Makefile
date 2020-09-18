
.PHONY: build
build: build-win

.PHONY: build-win
build-win:
	GOOS=windows GOARCH=amd64 go build -o build/twitter-calorie-win.exe main.go
