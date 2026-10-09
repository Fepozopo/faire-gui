package application

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// openTrackingURL opens one resolved official-carrier tracking URL and reports a safe in-page failure when the browser cannot be started.
// trackingURL originates from the allowlisted Orders presenter; it has no return value because failures are represented through the visible order-detail status.
func (ui *DesktopUI) openTrackingURL(trackingURL string) {
	if err := ui.openBrowserURL(trackingURL); err != nil {
		ui.orders.view.orderDetailStatus = "Could not open the carrier tracking website."
	}
	ui.invalidate()
}

// openShippingLabel opens the existing label at labelURL for printing in the user's browser or PDF viewer.
// labelURL comes from the validated Orders presenter; it returns no value and reports browser-launch success or a safe failure in the detail status without purchasing a new label.
func (ui *DesktopUI) openShippingLabel(labelURL string) {
	if err := ui.openBrowserURL(labelURL); err != nil {
		ui.orders.view.orderDetailStatus = "Could not open the shipping label. Try again or refresh the order."
	} else {
		ui.orders.view.orderDetailStatus = "Shipping label opened in your browser. Print it from the browser or PDF viewer."
	}
	ui.invalidate()
}

// openBrowserURL validates an HTTPS URL and delegates it to the operating system's default browser.
// rawURL must be an absolute HTTPS URL; it returns an error when validation fails or the OS cannot start its browser-opening command.
func openBrowserURL(rawURL string) error {
	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("parse browser URL: %w", err)
	}
	if parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return fmt.Errorf("browser URL must be an absolute HTTPS URL")
	}

	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// macOS's open utility asks Launch Services to use the user's default browser.
		command = exec.Command("open", rawURL)
	case "windows":
		// rundll32 delegates the URL protocol to the user's registered Windows browser handler without invoking a shell.
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		// xdg-open is the freedesktop standard dispatcher used by supported Linux desktop environments.
		command = exec.Command("xdg-open", rawURL)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start browser: %w", err)
	}
	return nil
}
