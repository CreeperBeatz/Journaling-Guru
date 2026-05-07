// Command genvapid prints a fresh VAPID key pair to stdout.
// Run once at setup; copy the output into .env. Re-running invalidates
// every existing subscription, so don't.
package main

import (
	"fmt"
	"os"

	"github.com/cosmosthrace/journai/backend/internal/push"
)

func main() {
	priv, pub, err := push.GenerateVAPIDKeys()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate vapid: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\n", pub)
	fmt.Printf("VAPID_PRIVATE_KEY=%s\n", priv)
	fmt.Printf("VAPID_SUBJECT=mailto:you@example.com\n")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Add the lines above to your .env (copy-paste).")
	fmt.Fprintln(os.Stderr, "VITE_VAPID_PUBLIC_KEY must equal VAPID_PUBLIC_KEY for the SPA.")
}
