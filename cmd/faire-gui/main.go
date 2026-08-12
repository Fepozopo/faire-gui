// Command faire-gui starts the Faire desktop application.
package main

import "github.com/Fepozopo/faire-gui/application"

// main starts the desktop application on the process main goroutine required by Gio.
func main() {
	application.Run()
}
