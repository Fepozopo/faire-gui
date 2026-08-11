// Command faire-gui starts the Faire desktop application.
package main

import (
	"log"

	"github.com/Fepozopo/faire-gui/application"
)

// main starts the desktop application and reports any window-runtime failure.
func main() {
	if err := application.Run(); err != nil {
		log.Fatal(err)
	}
}
