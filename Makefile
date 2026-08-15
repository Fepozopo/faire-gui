# Makefile builds the Faire desktop application for supported GitHub release platforms.

APP := faire-gui
CMD := ./cmd/faire-gui
BIN_DIR := bin

.PHONY: all darwin-arm64 windows-arm64 windows-amd64 clean

# all builds every release asset accepted by the in-app updater.
all: darwin-arm64 windows-arm64 windows-amd64

# darwin-arm64 builds the Apple Silicon executable uploaded as faire-gui_darwin_arm64.
darwin-arm64:
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 go build -o $(BIN_DIR)/$(APP)_darwin_arm64 $(CMD)

# windows-arm64 builds the Windows-on-ARM executable uploaded as faire-gui_windows_arm64.exe.
windows-arm64:
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=arm64 go build -ldflags="-H=windowsgui" -o $(BIN_DIR)/$(APP)_windows_arm64.exe $(CMD)

# windows-amd64 builds the 64-bit Windows executable uploaded as faire-gui_windows_amd64.exe.
windows-amd64:
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=amd64 go build -ldflags="-H=windowsgui" -o $(BIN_DIR)/$(APP)_windows_amd64.exe $(CMD)

# clean removes all generated build artifacts.
clean:
	rm -rf $(BIN_DIR)
