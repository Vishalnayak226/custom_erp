// Command qzcert exports the QZ Tray signing certificate.
//
// Silent printing needs each packing PC to trust the certificate this server
// signs print requests with. QZ Tray reads that trust from a file called
// override.crt in its install directory, so rolling this out is: run this
// once, copy the file to every PC, restart QZ Tray.
//
// Usage:
//
//	go run ./cmd/qzcert                 # print the certificate to stdout
//	go run ./cmd/qzcert -o override.crt # write it to a file
//
// The private key never leaves the server - this only ever emits the public
// certificate, which is safe to copy around and to commit to a deployment
// share if that is how the machines are managed.
package main

import (
	"custom_erp/engines"
	"flag"
	"fmt"
	"os"
)

func main() {
	out := flag.String("o", "", "write the certificate to this file instead of stdout")
	flag.Parse()

	cert, err := engines.QZCertificatePEM()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load the QZ signing certificate: %v\n", err)
		os.Exit(1)
	}

	if *out == "" {
		fmt.Print(cert)
		return
	}

	if err := os.WriteFile(*out, []byte(cert), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "could not write %s: %v\n", *out, err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %s\n\n", *out)
	fmt.Println("To make printing silent on a packing PC:")
	fmt.Println("  1. Install QZ Tray on that PC and make sure it is running.")
	fmt.Println("  2. Copy this file into QZ Tray's install directory as override.crt")
	fmt.Println(`     Windows: C:\Program Files\QZ Tray\override.crt`)
	fmt.Println("     macOS:   /Applications/QZ Tray.app/Contents/Resources/override.crt")
	fmt.Println("     Linux:   /opt/qz-tray/override.crt")
	fmt.Println("  3. Restart QZ Tray. Prints from the ERP no longer raise a prompt.")
}
