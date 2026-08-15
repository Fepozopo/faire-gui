# Makefile builds the Faire desktop application for supported release platforms.

APP := faire
CMD := ./cmd/faire-gui
BIN_DIR := bin

.PHONY: all macos-arm64 windows-arm64 windows-amd64 clean

# all builds every supported release artifact.
all: macos-arm64 windows-arm64 windows-amd64

# macos-arm64 builds a native macOS executable for Apple Silicon.
darwin-arm64:
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 go build -o $(BIN_DIR)/$(APP)-darwin-arm64 $(CMD)

# windows-arm64 builds a Windows-on-ARM executable without a console window.
windows-arm64:
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=arm64 go build -ldflags="-H=windowsgui" -o $(BIN_DIR)/$(APP)-windows-arm64.exe $(CMD)

# windows-amd64 builds a 64-bit Windows executable without a console window.
windows-amd64:
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=amd64 go build -ldflags="-H=windowsgui" -o $(BIN_DIR)/$(APP)-windows-amd64.exe $(CMD)

# clean removes all generated build artifacts.
clean:
	rm -rf $(BIN_DIR)
