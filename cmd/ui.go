package cmd

import (
	"fmt"
	"net/http"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/DHaussermann/ghquery/internal/web"
)

var uiPort int
var uiNoOpen bool

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Start the web UI",
	Long:  "Starts a local web server with a browser-based interface for building and running queries.",
	RunE:  runUI,
}

func init() {
	uiCmd.Flags().IntVar(&uiPort, "port", 8080, "port for the web server")
	uiCmd.Flags().BoolVar(&uiNoOpen, "no-open", false, "don't automatically open the browser")
	rootCmd.AddCommand(uiCmd)
}

func runUI(cmd *cobra.Command, args []string) error {
	shutdown := make(chan struct{})

	// Open browser unless --no-open
	if !uiNoOpen {
		url := fmt.Sprintf("http://localhost:%d", uiPort)
		go openBrowser(url)
	}

	err := web.StartServer(uiPort, shutdown)
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("web server failed: %w", err)
	}

	fmt.Println("Server stopped.")
	return nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		cmd = exec.Command("open", url)
	}
	cmd.Run()
}
